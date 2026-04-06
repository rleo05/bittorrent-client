package peer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	peerHandshakeTimeout   = 15 * time.Second
	peerHandshakeIOTimeout = 10 * time.Second

	maxInFlightRequests = 5
)

type Config struct {
	InfoHash           [20]byte
	PeerID             [20]byte
	PeerChan           chan types.PeerAddress
	PieceManager       *piece.Manager
	PieceCompletedChan <-chan int
}

type Manager struct {
	stats *types.Stats
	Config
	mu       sync.Mutex
	sessions map[*PeerSession]struct{}
}

func NewManager(stats *types.Stats, cfg Config) *Manager {
	return &Manager{
		stats:    stats,
		Config:   cfg,
		sessions: make(map[*PeerSession]struct{}),
	}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	maxPeers := make(chan struct{}, 50)

	for {
		select {
		case <-ctx.Done():
			return
		case pieceIndex, ok := <-m.PieceCompletedChan:
			if !ok {
				m.PieceCompletedChan = nil
				continue
			}

			m.broadcastHave(pieceIndex)
		case peer, ok := <-m.PeerChan:
			if !ok {
				log.Printf("peer manager stopped: reason=peer channel closed")
				return
			}

			maxPeers <- struct{}{}

			go func(peer types.PeerAddress) {
				defer func() { <-maxPeers }()

				m.handlePeer(ctx, peer)
			}(peer)
		}
	}
}

func (m *Manager) handlePeer(ctx context.Context, peer types.PeerAddress) {
	conn, err := m.doHandshake(ctx, peer)
	if err != nil {
		log.Printf("peer handshake failed: peer=%s error=%v", peer.String(), err)
		return
	}
	defer conn.Close()

	session := &PeerSession{
		conn:           conn,
		address:        peer.String(),
		bitfield:       nil,
		pieceManager:   m.PieceManager,
		amChoking:      true,
		amInterested:   false,
		peerChoking:    true,
		peerInterested: false,
		inFlightRequests: make(map[types.BlockKey]struct{}, maxInFlightRequests),
		msgChan:        make(chan PeerMessage),
		commandChan:    make(chan sessionCommand, 16),
		outboundChan:   make(chan []byte, 16),
	}

	m.registerSession(session)
	defer m.unregisterSession(session)

	session.Start(ctx)
}

func (m *Manager) doHandshake(ctx context.Context, peer types.PeerAddress) (net.Conn, error) {
	handshakeCtx, cancel := context.WithTimeout(ctx, peerHandshakeTimeout)
	defer cancel()

	address := peer.String()
	protocolName := "BitTorrent protocol"
	log.Printf("peer connection started: peer=%s", address)

	dialer := net.Dialer{Timeout: peerHandshakeTimeout}
	conn, err := dialer.DialContext(handshakeCtx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial peer %s: %w", address, err)
	}

	stop := context.AfterFunc(handshakeCtx, func() {
		conn.SetReadDeadline(time.Now())
		conn.SetWriteDeadline(time.Now())
	})
	defer stop()

	handshakeBuf := make([]byte, 68)
	handshakeBuf[0] = 19
	copy(handshakeBuf[1:20], protocolName)
	copy(handshakeBuf[28:48], m.InfoHash[:])
	copy(handshakeBuf[48:68], m.PeerID[:])

	conn.SetWriteDeadline(time.Now().Add(peerHandshakeIOTimeout))
	_, err = conn.Write(handshakeBuf)
	if err != nil {
		return nil, fmt.Errorf("send handshake to peer %s: %w", address, err)
	}
	log.Printf("peer handshake sent: peer=%s", address)

	conn.SetReadDeadline(time.Now().Add(peerHandshakeIOTimeout))
	responseBuf := make([]byte, 68)
	_, err = io.ReadFull(conn, responseBuf)
	if err != nil {
		return nil, fmt.Errorf("read handshake from peer %s: %w", address, err)
	}

	if responseBuf[0] != 19 {
		return nil, fmt.Errorf("invalid protocol length from peer %s: got=%d expected=19", address, responseBuf[0])
	}

	if p := string(responseBuf[1:20]); p != protocolName {
		return nil, fmt.Errorf("invalid protocol name from peer %s: expected=%q got=%q", address, protocolName, p)
	}

	peerInfoHash := responseBuf[28:48]
	if !bytes.Equal(peerInfoHash, m.InfoHash[:]) {
		return nil, fmt.Errorf("info hash mismatch from peer %s: got=%x", address, peerInfoHash)
	}

	_ = responseBuf[48:68] // peerID
	log.Printf("peer handshake completed: peer=%s", address)

	return conn, nil
}

func (s *PeerSession) Start(ctx context.Context) {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := s.messageReader(); err != nil {
			log.Printf("peer session stopped: peer=%s error=%v", s.address, err)
			cancel()
		}
	}()
	go func() {
		if err := s.messageWriter(sessionCtx); err != nil {
			log.Printf("peer session stopped: peer=%s error=%v", s.address, err)
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
			return fmt.Errorf("read message length from peer %s: %w", s.address, err)
		}

		msgLength := binary.BigEndian.Uint32(lengthBuf)

		if msgLength == 0 {
			log.Printf("peer keepalive received: peer=%s", s.address)
			continue
		}

		msgBuf := make([]byte, msgLength)
		_, err = io.ReadFull(s.conn, msgBuf)
		if err != nil {
			return fmt.Errorf("read message payload from peer %s: length=%d error=%w", s.address, msgLength, err)
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
			s.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			_, err := s.conn.Write(payload)
			if err != nil {
				return fmt.Errorf("write message to peer %s: %w", s.address, err)
			}
		}
	}
}

func (p *PeerSession) stateMachine(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-p.msgChan:
			switch msg.messageStatus {
			case Choke:
			case Unchoke:
			case Interested:
			case NotInterested:
			case Have:
			case Bitfield:
			case Request:
			case Piece:
			case Cancel:
			}
		case command := <-p.commandChan:
			switch command.commandType {
			case sessionCommandSendHave:
				p.enqueueHave(command.pieceIndex)
			}
		}
	}
}

func (m *Manager) registerSession(session *PeerSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[session] = struct{}{}
}

func (m *Manager) unregisterSession(session *PeerSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, session)
}

func (m *Manager) broadcastHave(pieceIndex int) {
	m.mu.Lock()
	sessions := make([]*PeerSession, 0, len(m.sessions))
	for session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	command := sessionCommand{commandType: sessionCommandSendHave, pieceIndex: pieceIndex}
	for _, session := range sessions {
		select {
		case session.commandChan <- command:
		default:
		}
	}
}

func (s *PeerSession) enqueueHave(pieceIndex int) {
	payload := make([]byte, 9)
	binary.BigEndian.PutUint32(payload[0:4], 5)
	payload[4] = byte(Have)
	binary.BigEndian.PutUint32(payload[5:9], uint32(pieceIndex))

	s.outboundChan <- payload
}
