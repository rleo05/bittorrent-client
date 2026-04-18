package disk

import (
	"context"
	"fmt"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

type Config struct {
	WriteChan   chan shared.DiskWrite
	Name        string
	Length      *uint64
	PieceLength uint64
	Files       *[]shared.File
}

type Manager struct {
	Config
}

func NewManager(cfg Config) *Manager {
	return &Manager{Config: cfg}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <- ctx.Done():
			return	
		case _, ok := <-m.WriteChan:
			if !ok {
				return
			}

			fmt.Println("writing block")
		}
	}
}
