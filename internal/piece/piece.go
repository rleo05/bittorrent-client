package piece

import (
	"context"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	maxBlockSize = 16384
)

type Config struct {
	WriteChan   chan types.DiskWrite
	PieceLength uint64
	Pieces      []byte
	TotalLength uint64
}

type Manager struct {
	Config

	bitfield       []byte
	totalPieces    int
	pieces         []*types.PieceState
	inFlightBlocks map[types.InFlightKey]types.InFlightValue

	PieceCompletedChan chan int
}

func NewManager(cfg Config) *Manager {
	totalPieces := len(cfg.Pieces) / 20
	manager := &Manager{
		Config:             cfg,
		PieceCompletedChan: make(chan int, 100),
		bitfield:           make([]byte, (totalPieces+7)/8),
		totalPieces:        totalPieces,
		inFlightBlocks:     make(map[types.InFlightKey]types.InFlightValue),
	}

	manager.initializePieces()

	return manager
}

func (m *Manager) initializePieces() {
	pieces := make([]*types.PieceState, m.totalPieces)
	for i := 0; i < m.totalPieces; i++ {
		currentPieceLength := int(m.PieceLength)
		if i == m.totalPieces-1 {
			currentPieceLength = int(m.TotalLength) % int(m.PieceLength)
			if currentPieceLength == 0 {
				currentPieceLength = int(m.PieceLength)
			}
		}

		var blocks []types.Block
		offset := 0
		bytesLeft := currentPieceLength 

		for bytesLeft > 0 {
			blockSize := min(bytesLeft, maxBlockSize)

			blocks = append(blocks, types.Block{
				PieceIndex:  i,
				Offset:      offset,
				Size:        blockSize,
				Status:      types.Missing,
			})

			offset += blockSize
			bytesLeft -= blockSize
		}

		hashStart := i * 20
		hashEnd := hashStart + 20
		var hash [20]byte
		copy(hash[:], m.Pieces[hashStart:hashEnd])

		piece := &types.PieceState{
			Index:           i,
			Length:          currentPieceLength,
			Status:          types.Pending,
			Hash:            hash,
			Data:            nil,
			Blocks:          blocks,
			ReceivedBlocks:  0,
			RequestedBlocks: 0,
		}

		pieces[i] = piece
	}

	m.pieces = pieces
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

}
