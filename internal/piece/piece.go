package piece

import (
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	maxBlockSize           = 16384
	inFlightRequestTimeout = 30 * time.Second
)

type Config struct {
	WriteChan   chan types.DiskWrite
	PieceLength uint64
	Pieces      []byte
	TotalLength uint64
}

type Manager struct {
	Config

	bitfield        []byte
	TotalPieces     int
	pieces          []*types.PieceState
	inFlightBlocks  map[types.InFlightKey]*types.InFlightValue
	activePieces    map[int]*types.PieceState
	activePiecesOrd []int

	PieceCompletedChan chan int

	mu sync.Mutex
}

func NewManager(cfg Config) *Manager {
	totalPieces := len(cfg.Pieces) / 20
	manager := &Manager{
		Config:             cfg,
		PieceCompletedChan: make(chan int, 100),
		bitfield:           make([]byte, (totalPieces+7)/8),
		TotalPieces:        totalPieces,
		inFlightBlocks:     make(map[types.InFlightKey]*types.InFlightValue),
		activePieces:       make(map[int]*types.PieceState, 30),
	}

	manager.initializePieces()

	return manager
}

func (m *Manager) initializePieces() {
	pieces := make([]*types.PieceState, m.TotalPieces)
	for i := 0; i < m.TotalPieces; i++ {
		currentPieceLength := int(m.PieceLength)
		if i == m.TotalPieces-1 {
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
				PieceIndex: i,
				Offset:     offset,
				Length:     blockSize,
				Status:     types.Missing,
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

func (m *Manager) HasInterestingPieces(bitfield []byte) bool {
	if bitfield == nil {
		return false
	}

	for i := range bitfield {
		if (bitfield[i] &^ m.bitfield[i]) != 0 {
			return true
		}
	}
	return false
}

func (m *Manager) IsPeerBitfieldValid(bitfield []byte) bool {
	if len(bitfield) != len(m.bitfield) {
		return false
	}

	validBitsLastByte := m.TotalPieces % 8
	if validBitsLastByte == 0 {
		return true
	}

	extraBits := 8 - validBitsLastByte
	mask := byte((1 << extraBits) - 1)

	lastByte := bitfield[len(bitfield)-1]
	return lastByte&mask == 0
}

func (m *Manager) GetEmptyBitfield() []byte {
	return make([]byte, len(m.bitfield))
}

func (m *Manager) ReleaseInFlightRequest(key types.InFlightKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.releaseInFlightRequestLocked(key, true)
}

func (m *Manager) FillPeerInFlightRequests(bitfield []byte, maxInFlightRequests int, sessionID int64) []types.BlockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	blocks := make([]types.BlockRequest, 0, maxInFlightRequests)

	for _, pieceIndex := range m.activePiecesOrd {
		piece := m.activePieces[pieceIndex]
		if piece == nil {
			continue
		}

		if !m.hasPieceByIndex(bitfield, pieceIndex) {
			continue
		}

		for _, b := range piece.Blocks {
			if len(blocks) >= maxInFlightRequests {
				return blocks
			}

			if req, ok := m.createRequestBlock(b, sessionID); ok {
				blocks = append(blocks, req)
			}
		}
	}

	for pieceIndex, p := range m.pieces {
		if p.Status != types.Pending {
			continue
		}

		if !m.hasPieceByIndex(bitfield, pieceIndex) {
			continue
		}

		if _, exists := m.activePieces[pieceIndex]; exists {
			continue
		}

		for _, b := range p.Blocks {
			if len(blocks) >= maxInFlightRequests {
				return blocks
			}

			if req, ok := m.createRequestBlock(b, sessionID); ok {
				blocks = append(blocks, req)
				m.addActivePiece(pieceIndex, p)
			}
		}
	}

	return blocks
}

func (m *Manager) hasPieceByIndex(bitfield []byte, i int) bool {
	if i < 0 {
		return false
	}

	byteIndex := i / 8
	bitIndex := i % 8

	if byteIndex >= len(bitfield) {
		return false
	}

	currByte := bitfield[byteIndex]
	return currByte&(1<<(7-bitIndex)) != 0
}

func (m *Manager) createRequestBlock(b types.Block, sessionID int64) (types.BlockRequest, bool) {
	inFlightKey := types.InFlightKey{
		PieceIndex: b.PieceIndex,
		Offset:     b.Offset,
	}

	if b.Status == types.Received {
		return types.BlockRequest{}, false
	}

	if inFlight := m.inFlightBlocks[inFlightKey]; inFlight != nil {
		if time.Since(inFlight.RequestedAt) < inFlightRequestTimeout {
			return types.BlockRequest{}, false
		}

		m.releaseInFlightRequestLocked(inFlightKey, false)
	}

	m.inFlightBlocks[inFlightKey] = &types.InFlightValue{
		SessionID:   sessionID,
		RequestedAt: time.Now(),
	}

	m.pieces[b.PieceIndex].RequestedBlocks++	

	blockRequest := types.BlockRequest{
		PieceIndex: b.PieceIndex,
		Offset:     b.Offset,
		Length:     uint32(b.Length),
	}

	return blockRequest, true
}

func (m *Manager) addActivePiece(pieceIndex int, pieceState *types.PieceState) {
	if _, ok := m.activePieces[pieceIndex]; ok {
		return
	}

	m.activePieces[pieceIndex] = pieceState
	m.activePiecesOrd = append(m.activePiecesOrd, pieceIndex)
}

func (m *Manager) removeActivePiece(pieceIndex int) {
	if _, ok := m.activePieces[pieceIndex]; !ok {
		return
	}

	delete(m.activePieces, pieceIndex)

	for i, idx := range m.activePiecesOrd {
		if idx != pieceIndex {
			continue
		}

		m.activePiecesOrd = append(m.activePiecesOrd[:i], m.activePiecesOrd[i+1:]...)
		return
	}
}

func (m *Manager) releaseInFlightRequestLocked(key types.InFlightKey, removeActivePiece bool) {
	if _, ok := m.inFlightBlocks[key]; !ok {
		return
	}

	delete(m.inFlightBlocks, key)

	piece := m.pieces[key.PieceIndex]
	
	if piece.RequestedBlocks > 0 {
		piece.RequestedBlocks--
	}

	if removeActivePiece &&
		piece.Status == types.Pending &&
		piece.RequestedBlocks == 0 &&
		piece.ReceivedBlocks == 0 {
		m.removeActivePiece(key.PieceIndex)
	}
}