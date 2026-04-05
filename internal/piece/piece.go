package piece

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	WriteChan   chan types.DiskWrite
	PieceLength uint64
	Pieces      []byte
	TotalLength uint64
}

type Manager struct {
	Config
	PieceCompletedChan chan int
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		Config:             cfg,
		PieceCompletedChan: make(chan int, 100),
	}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

}