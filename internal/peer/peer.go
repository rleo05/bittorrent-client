package peer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/shared"
)

const (
	peerHandshakeTimeout   = 5 * time.Second
	peerHandshakeIOTimeout = 10 * time.Second
	protocolName           = "BitTorrent protocol"
)

type Config struct {
	InfoHash           [20]byte
	PeerID             [20]byte
	PeerChan           chan shared.PeerAddress
	PieceManager       *piece.Manager
	PieceCompletedChan <-chan int
}

type Manager struct {
	stats *shared.Stats
	Config
	mu       sync.Mutex
	sessions map[int]*PeerSession
}

var (
	counterSessionID int = 1
)

func NewManager(stats *shared.Stats, cfg Config) *Manager {
	return &Manager{
		stats:    stats,
		Config:   cfg,
		sessions: make(map[int]*PeerSession),
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

			go func(peer shared.PeerAddress) {
				defer func() { <-maxPeers }()

				m.handlePeer(ctx, peer)
			}(peer)
		}
	}
}

func (m *Manager) handlePeer(ctx context.Context, peer shared.PeerAddress) {
	conn, err := m.doHandshake(ctx, peer)
	if err != nil {
		log.Printf("peer handshake failed: peer=%s error=%v", peer.String(), err)
		return
	}
	defer conn.Close()

	session := newSession(conn, peer.String(), m.PieceManager)

	sessionID := m.registerSession(session)
	defer m.unregisterSession(sessionID)

	session.Start(ctx)
}

func (m *Manager) doHandshake(ctx context.Context, peer shared.PeerAddress) (net.Conn, error) {
	handshakeCtx, cancel := context.WithTimeout(ctx, peerHandshakeTimeout)
	defer cancel()

	address := peer.String()
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

func (m *Manager) registerSession(session *PeerSession) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionID := counterSessionID

	m.sessions[counterSessionID] = session

	counterSessionID++

	return sessionID
}

func (m *Manager) unregisterSession(sessionID int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sessionID)
}

func (m *Manager) broadcastHave(pieceIndex int) {
	m.mu.Lock()
	sessions := make([]*PeerSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	command := sessionCommand{commandType: sendHave, pieceIndex: pieceIndex}
	for _, session := range sessions {
		select {
		case session.commandChan <- command:
		default:
		}
	}
}
