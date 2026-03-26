package torrent

import (
	"github.com/rleo05/bittorrent-client/internal/tracker"
	"github.com/rleo05/bittorrent-client/internal/peer"	
	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/disk"
)

type Session struct {
	torrent *Torrent
	Stats *Stats

	trackerManager tracker.Manager
	peerManager peer.Manager
	pieceManager piece.Manager
	diskManager disk.Manager

	peerChan chan peer.Peer
	responseBlockChan chan piece.BlockResponse
	requestBlockChan chan piece.BlockRequest
	writeChan chan disk.DiskWrite
}

type Stats struct {
	downloaded int64
	uploaded int64
	left int64
}

func NewSession(data *Torrent) *Session {
	var session = &Session{Stats: 
		&Stats{
			downloaded: 0,
			uploaded: 0,
			left: data.TotalLength,
		},
	}

	return session
}


func (s *Session) Start() {
	
}