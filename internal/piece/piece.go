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

	bitfield []byte
	totalPieces int
	pieces []types.PieceState
	inFlightBlocks map[types.BlockKey]struct{}
	
	PieceCompletedChan chan int
}

func NewManager(cfg Config) *Manager {
	totalPieces := len(cfg.Pieces) / 20
	return &Manager{
		Config:             cfg,
		PieceCompletedChan: make(chan int, 100),
		bitfield: make([]byte, (totalPieces + 7) / 8),
		totalPieces: totalPieces,
		inFlightBlocks: make(map[types.BlockKey]struct{}),
	}
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

}