package piece

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	ResponseBlockChan chan types.BlockResponse
	RequestBlockChan  chan types.BlockRequest
	WriteChan         chan types.DiskWrite
	PieceLength       uint64
	Pieces            []byte
	TotalLength       uint64
}

type Manager struct {
	Config
}

func NewManager(cfg Config) *Manager {
	return &Manager{Config: cfg}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	
}