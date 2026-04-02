package torrent

import (
	"context"
	"crypto/rand"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/disk"
	"github.com/rleo05/bittorrent-client/internal/peer"
	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/tracker"
	"github.com/rleo05/bittorrent-client/internal/types"
)

type Session struct {
	torrent *Torrent
	Stats   *types.Stats
	peerID  [20]byte

	trackerManager *tracker.Manager
	peerManager    *peer.Manager
	pieceManager   *piece.Manager
	diskManager    *disk.Manager

	peerChan          chan types.PeerAddress
	responseBlockChan chan types.BlockResponse
	requestBlockChan  chan types.BlockRequest
	writeChan         chan types.DiskWrite
}

func generatePeerID() [20]byte {
	prefix := "-BC0001-"

	var peerID [20]byte
	copy(peerID[:8], prefix)

	rand.Read(peerID[8:])

	return peerID
}

func NewSession(data *Torrent, port uint16) *Session {
	stats := &types.Stats{}
	stats.Left.Store(data.TotalLength)
	stats.Downloaded.Store(0)
	stats.Uploaded.Store(0)

	session := &Session{
		Stats:             stats,
		peerID:            generatePeerID(),
		peerChan:          make(chan types.PeerAddress, 200),
		responseBlockChan: make(chan types.BlockResponse, 1000),
		requestBlockChan:  make(chan types.BlockRequest, 500),
		writeChan:         make(chan types.DiskWrite, 50),
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

	session.peerManager = peer.NewManager(stats, peer.Config{
		InfoHash:          data.InfoHash,
		PeerID:            session.peerID,
		PeerChan:          session.peerChan,
		ResponseBlockChan: session.responseBlockChan,
		RequestBlockChan:  session.requestBlockChan,
	})
	session.pieceManager = piece.NewManager(piece.Config{
		ResponseBlockChan: session.responseBlockChan,
		RequestBlockChan:  session.requestBlockChan,
		WriteChan:         session.writeChan,
		PieceLength:       data.Info.PieceLength,
		Pieces:            data.Info.Pieces,
		TotalLength:       data.TotalLength,
	})
	session.diskManager = disk.NewManager(disk.Config{
		WriteChan:   session.writeChan,
		Name:        data.Info.Name,
		Length:      data.Info.Length,
		PieceLength: data.Info.PieceLength,
		Files:       data.Info.Files,
	})

	return session
}

func (s *Session) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(4)
	go s.trackerManager.Run(ctx, wg)
	go s.peerManager.Run(ctx, wg)
	go s.pieceManager.Run(ctx, wg)
	go s.diskManager.Run(ctx, wg)
}
