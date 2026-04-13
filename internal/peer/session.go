package peer

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	maxInFlightRequests = 5
)

func newSession(conn net.Conn, address string, pieceManager *piece.Manager) *PeerSession {
	return &PeerSession{
		conn:             conn,
		address:          address,
		bitfield:         nil,
		pieceManager:     pieceManager,
		amChoking:        true,
		amInterested:     false,
		peerChoking:      true,
		peerInterested:   false,
		seenFirstMessage: false,
		inFlightRequests: make(map[types.InFlightKey]int, maxInFlightRequests),
		msgChan:          make(chan PeerMessage, 32),
		commandChan:      make(chan sessionCommand, 16),
		outboundChan:     make(chan []byte, 16),
	}
}

func (s *PeerSession) peerErrorf(format string, args ...any) error {
	args = append([]any{s.address}, args...)
	return fmt.Errorf("peer=%s "+format, args...)
}

func (s *PeerSession) logPeerf(format string, args ...any) {
	args = append([]any{s.address}, args...)
	log.Printf("peer=%s "+format, args...)
}

func (s *PeerSession) logSessionError(err error) {
	log.Printf("peer session stopped: %v", err)
}

func (s *PeerSession) Start(ctx context.Context) {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := s.messageReader(); err != nil {
			s.logSessionError(err)
			cancel()
		}
	}()
	go func() {
		if err := s.messageWriter(sessionCtx); err != nil {
			s.logSessionError(err)
			cancel()
		}
	}()

	s.stateMachine(sessionCtx)
}

func (s *PeerSession) messageReader() error {
	for {
		s.conn.SetReadDeadline(time.Now().Add(2 * time.Minute))

		lengthBuf := make([]byte, 4)
		_, err := io.ReadFull(s.conn, lengthBuf)
		if err != nil {
			return s.peerErrorf("read message length: %w", err)
		}

		msgLength := binary.BigEndian.Uint32(lengthBuf)

		if msgLength == 0 {
			s.logPeerf("keepalive received")
			continue
		}

		msgBuf := make([]byte, msgLength)
		_, err = io.ReadFull(s.conn, msgBuf)
		if err != nil {
			return s.peerErrorf("read message payload: length=%d error=%w", msgLength, err)
		}

		messageStatus := MessageStatus(msgBuf[0])
		payload := msgBuf[1:]

		s.msgChan <- PeerMessage{messageStatus: messageStatus, payload: payload}
	}
}

func (s *PeerSession) messageWriter(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case payload := <-s.outboundChan:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_, err := s.conn.Write(payload)
			if err != nil {
				return s.peerErrorf("write message: %w", err)
			}
		}
	}
}
