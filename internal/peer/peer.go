package peer

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	InfoHash          [20]byte
	PeerID            [20]byte
	PeerChan          chan types.PeerAddress
	ResponseBlockChan chan types.BlockResponse
	RequestBlockChan  chan types.BlockRequest
}

type Manager struct {
	stats *types.Stats
	Config
}

func NewManager(stats *types.Stats, cfg Config) *Manager {
	return &Manager{
		stats:  stats,
		Config: cfg,
	}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	
}

