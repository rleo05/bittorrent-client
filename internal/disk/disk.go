package disk

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	WriteChan   chan types.DiskWrite
	Name        string
	Length      *int64
	PieceLength int64
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
