package torrent

import (
	"context"
	"crypto/rand"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/disk"
	"github.com/rleo05/bittorrent-client/internal/peer"
	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/shared"
	"github.com/rleo05/bittorrent-client/internal/tracker"
)

type Session struct {
	torrent *Torrent
	Stats   *shared.Stats
	peerID  [20]byte

	trackerManager *tracker.Manager
	peerManager    *peer.Manager
	pieceManager   *piece.Manager
	diskManager    *disk.Manager

	peerChan        chan shared.PeerAddress
	writeChan       chan *shared.DiskWrite
	writeResultChan chan *shared.DiskWriteResult
	completedChan   chan struct{}
	completeAckChan chan struct{}
}

func generatePeerID() [20]byte {
	prefix := "-BC0001-"

	var peerID [20]byte
	copy(peerID[:8], prefix)

	rand.Read(peerID[8:])

	return peerID
}

func NewSession(data *Torrent, port uint16, outputRoot string) *Session {
	stats := &shared.Stats{}
	stats.Left.Store(data.TotalLength)
	stats.Downloaded.Store(0)
	stats.Uploaded.Store(0)

	session := &Session{
		Stats:           stats,
		peerID:          generatePeerID(),
		peerChan:        make(chan shared.PeerAddress, 200),
		writeChan:       make(chan *shared.DiskWrite, 50),
		writeResultChan: make(chan *shared.DiskWriteResult, 1),
		completedChan:   make(chan struct{}, 1),
		completeAckChan: make(chan struct{}, 1),
	}

	session.trackerManager = tracker.NewManager(
		stats,
		tracker.Config{
			InfoHash:     data.InfoHash,
			PeerID:       session.peerID,
			Announce:     data.Announce,
			AnnounceList: data.AnnounceList,
			PeerChan:     session.peerChan,
			Completed:    session.completedChan,
			CompleteAck:  session.completeAckChan,
			Port:         port,
		})

	session.pieceManager = piece.NewManager(piece.Config{
		WriteChan:       session.writeChan,
		WriteResultChan: session.writeResultChan,
		PieceLength:     data.Info.PieceLength,
		Pieces:          data.Info.Pieces,
		TotalLength:     data.TotalLength,
	})

	session.peerManager = peer.NewManager(stats, peer.Config{
		InfoHash:           data.InfoHash,
		PeerID:             session.peerID,
		PeerChan:           session.peerChan,
		PieceManager:       session.pieceManager,
		PieceCompletedChan: session.pieceManager.PieceCompletedChan,
	})
	session.diskManager = disk.NewManager(disk.Config{
		WriteChan:       session.writeChan,
		WriteResultChan: session.writeResultChan,
		Stats:           stats,
		Name:            data.Info.Name,
		Length:          data.Info.Length,
		PieceLength:     data.Info.PieceLength,
		Files:           data.Info.Files,
		OutputRoot:      outputRoot,
		Completed:       session.completedChan,
		CompleteAck:     session.completeAckChan,
	})

	return session
}

func (s *Session) Start(ctx context.Context, wg *sync.WaitGroup) error {
	if err := s.diskManager.Prepare(); err != nil {
		return err
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	s.diskManager.CancelSession = cancel

	wg.Add(4)
	go s.trackerManager.Run(sessionCtx, wg)
	go s.peerManager.Run(sessionCtx, wg)
	go s.pieceManager.Run(sessionCtx, wg)
	go s.diskManager.Run(sessionCtx, wg)

	return nil
}
