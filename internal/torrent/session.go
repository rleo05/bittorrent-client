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

	peerChan  chan shared.PeerAddress
	writeChan chan shared.DiskWrite
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
		Stats:     stats,
		peerID:    generatePeerID(),
		peerChan:  make(chan shared.PeerAddress, 200),
		writeChan: make(chan shared.DiskWrite, 50),
	}

	session.trackerManager = tracker.NewManager(
		stats,
		tracker.Config{
			InfoHash:     data.InfoHash,
			PeerID:       session.peerID,
			Announce:     data.Announce,
			AnnounceList: data.AnnounceList,
			PeerChan:     session.peerChan,
			Port:         port,
		})

	session.pieceManager = piece.NewManager(piece.Config{
		WriteChan:   session.writeChan,
		PieceLength: data.Info.PieceLength,
		Pieces:      data.Info.Pieces,
		TotalLength: data.TotalLength,
	})

	session.peerManager = peer.NewManager(stats, peer.Config{
		InfoHash:           data.InfoHash,
		PeerID:             session.peerID,
		PeerChan:           session.peerChan,
		PieceManager:       session.pieceManager,
		PieceCompletedChan: session.pieceManager.PieceCompletedChan,
	})
	session.diskManager = disk.NewManager(disk.Config{
		WriteChan:   session.writeChan,
		Name:        data.Info.Name,
		Length:      data.Info.Length,
		PieceLength: data.Info.PieceLength,
		Files:       data.Info.Files,
		OutputRoot: outputRoot,
	})

	return session
}

func (s *Session) Start(ctx context.Context, wg *sync.WaitGroup) error {
	if err := s.diskManager.Prepare(); err != nil {
		return err
	}

	wg.Add(3)
	go s.trackerManager.Run(ctx, wg)
	go s.peerManager.Run(ctx, wg)
	go s.diskManager.Run(ctx, wg)

	return nil
}
