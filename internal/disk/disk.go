package disk

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	WriteChan   chan types.DiskWrite
	Name        string
	Length      *uint64
	PieceLength uint64
	Files       *[]types.File
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
