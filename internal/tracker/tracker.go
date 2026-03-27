package tracker

import (
	"context"
	"net/url"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	InfoHash     [20]byte
	PeerID       [20]byte
	Announce     *url.URL
	AnnounceList [][]*url.URL
	PeerChan     chan types.PeerAddress
	Port         int
}

type Request struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string
}

type Manager struct {
	*types.Stats
	Config
}

func NewManager(stats *types.Stats, cfg Config) *Manager {
	return &Manager{
		Stats:  stats,
		Config: cfg,
	}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	request := Request{
		InfoHash:   m.InfoHash,
		PeerID:     m.PeerID,
		Port:       m.Port,
		Uploaded:   m.Uploaded.Load(),
		Downloaded: m.Downloaded.Load(),
		Left:       m.Left.Load(),
		Event:      "started",
	}

	switch m.Announce.Scheme {
	case "udp":
		handleUdpRequest(request)
	case "http", "https":
		handleHttpRequest(request)
	}
}

func handleUdpRequest(request Request) {

}

func handleHttpRequest(request Request) {

}
