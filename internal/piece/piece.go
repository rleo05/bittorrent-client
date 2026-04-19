package piece

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

const (
	maxBlockSize           = 16384
	inFlightRequestTimeout = 30 * time.Second
)

type Config struct {
	WriteChan       chan *shared.DiskWrite
	WriteResultChan chan *shared.DiskWriteResult
	PieceLength     uint64
	Pieces          []byte
	TotalLength     uint64
}

type Manager struct {
	Config

	bitfield        []byte
	TotalPieces     int
	pieces          []*shared.PieceState
	inFlightBlocks  map[shared.InFlightKey]*shared.InFlightValue
	activePieces    map[int]*shared.PieceState
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
		inFlightBlocks:     make(map[shared.InFlightKey]*shared.InFlightValue),
		activePieces:       make(map[int]*shared.PieceState, 30),
	}

	manager.initializePieces()

	return manager
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-m.WriteResultChan:
			if !ok {
				return
			}

			if v.Error != nil {
				log.Printf("piece manager stopped: disk write failed for piece=%d error=%v", v.PieceIndex, v.Error)
				return
			}

			m.PieceCompletedChan <- v.PieceIndex
		}
	}

}

func (m *Manager) initializePieces() {
	pieces := make([]*shared.PieceState, m.TotalPieces)
	for i := 0; i < m.TotalPieces; i++ {
		currentPieceLength := int(m.PieceLength)
		if i == m.TotalPieces-1 {
			currentPieceLength = int(m.TotalLength) % int(m.PieceLength)
			if currentPieceLength == 0 {
				currentPieceLength = int(m.PieceLength)
			}
		}

		var blocks []*shared.Block
		offset := 0
		bytesLeft := currentPieceLength

		for bytesLeft > 0 {
			blockSize := min(bytesLeft, maxBlockSize)

			blocks = append(blocks, &shared.Block{
				PieceIndex: i,
				Offset:     offset,
				Length:     blockSize,
				Status:     shared.Missing,
			})

			offset += blockSize
			bytesLeft -= blockSize
		}

		hashStart := i * 20
		hashEnd := hashStart + 20
		var hash [20]byte
		copy(hash[:], m.Pieces[hashStart:hashEnd])

		piece := &shared.PieceState{
			Index:           i,
			Length:          currentPieceLength,
			Status:          shared.Pending,
			Hash:            hash,
			Data:            nil,
			Blocks:          blocks,
			ReceivedBlocks:  0,
			RequestedBlocks: 0,
			NeededBlocks:    len(blocks),
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

func (m *Manager) ReleaseInFlightRequest(key shared.InFlightKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.releaseInFlightRequestLocked(key, true)
}

func (m *Manager) FillPeerInFlightRequests(bitfield []byte, maxInFlightRequests int, sessionID int64) []shared.BlockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	blocks := make([]shared.BlockRequest, 0, maxInFlightRequests)

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
		if p.Status != shared.Pending {
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

func (m *Manager) createRequestBlock(b *shared.Block, sessionID int64) (shared.BlockRequest, bool) {
	inFlightKey := shared.InFlightKey{
		PieceIndex: b.PieceIndex,
		Offset:     b.Offset,
	}

	if b.Status == shared.Received {
		return shared.BlockRequest{}, false
	}

	if inFlight := m.inFlightBlocks[inFlightKey]; inFlight != nil {
		if time.Since(inFlight.RequestedAt) < inFlightRequestTimeout {
			return shared.BlockRequest{}, false
		}

		m.releaseInFlightRequestLocked(inFlightKey, false)
	}

	m.inFlightBlocks[inFlightKey] = &shared.InFlightValue{
		SessionID:   sessionID,
		RequestedAt: time.Now(),
	}

	m.pieces[b.PieceIndex].RequestedBlocks++

	blockRequest := shared.BlockRequest{
		PieceIndex: b.PieceIndex,
		Offset:     b.Offset,
		Length:     uint32(b.Length),
	}

	return blockRequest, true
}

func (m *Manager) addActivePiece(pieceIndex int, pieceState *shared.PieceState) {
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

func (m *Manager) releaseInFlightRequestLocked(key shared.InFlightKey, removeActivePiece bool) {
	if _, ok := m.inFlightBlocks[key]; !ok {
		return
	}

	delete(m.inFlightBlocks, key)

	piece := m.pieces[key.PieceIndex]

	if piece.RequestedBlocks > 0 {
		piece.RequestedBlocks--
	}

	if removeActivePiece &&
		piece.Status == shared.Pending &&
		piece.RequestedBlocks == 0 &&
		piece.ReceivedBlocks == 0 {
		m.removeActivePiece(key.PieceIndex)
	}
}

func (m *Manager) HandleReceivedBlock(pieceIndex uint32, begin uint32, block []byte) error {
	m.mu.Lock()

	piece, storedBlock, err := m.validateBlockLocked(pieceIndex, begin, len(block))
	if err != nil {
		m.mu.Unlock()
		return err
	}

	if piece == nil || storedBlock == nil {
		m.mu.Unlock()
		return nil
	}

	if piece.Data == nil {
		piece.Data = make([]byte, piece.Length)
	}

	copy(piece.Data[storedBlock.Offset:storedBlock.Offset+storedBlock.Length], block)

	storedBlock.Status = shared.Received
	piece.ReceivedBlocks++

	var completed bool

	if piece.ReceivedBlocks == piece.NeededBlocks {
		if m.validateHashLocked(piece) {
			byteIndex := piece.Index / 8
			bitIndex := piece.Index % 8
			m.bitfield[byteIndex] |= byte(1 << (7 - bitIndex))
			m.removeActivePiece(piece.Index)

			completed = true
		}
	}

	m.mu.Unlock()

	if completed {
		diskWrite := &shared.DiskWrite{
			PieceIndex: piece.Index,
			Offset:     int64(piece.Index) * int64(m.PieceLength),
			Data:       append([]byte(nil), piece.Data...),
		}

		m.WriteChan <- diskWrite
	}

	return nil
}

func (m *Manager) validateBlockLocked(pieceIndex uint32, begin uint32, blockLen int) (*shared.PieceState, *shared.Block, error) {
	if int(pieceIndex) >= m.TotalPieces {
		return nil, nil, fmt.Errorf("invalid piece index")
	}

	piece := m.pieces[int(pieceIndex)]

	if piece.Status != shared.Pending {
		return nil, nil, nil
	}

	if int(begin)+blockLen > piece.Length {
		return nil, nil, fmt.Errorf("invalid block boundaries: exceeds piece length")
	}

	blockIndex := int(begin / maxBlockSize)
	if blockIndex >= len(piece.Blocks) {
		return nil, nil, fmt.Errorf("invalid block index")
	}

	storedBlock := piece.Blocks[blockIndex]

	if storedBlock.Offset != int(begin) {
		return nil, nil, fmt.Errorf("invalid block offset: expected %d, got %d", storedBlock.Offset, begin)
	}

	if storedBlock.Length != blockLen {
		return nil, nil, fmt.Errorf("invalid block length: expected %d, got %d", storedBlock.Length, blockLen)
	}

	if storedBlock.Status != shared.Missing {
		return nil, nil, nil
	}

	return piece, storedBlock, nil
}

func (m *Manager) validateHashLocked(piece *shared.PieceState) bool {
	if piece.Data == nil || len(piece.Data) != piece.Length {
		return false
	}

	receivedHash := sha1.Sum(piece.Data)
	if receivedHash != piece.Hash {
		piece.Data = nil
		for _, b := range piece.Blocks {
			b.Status = shared.Missing
		}

		piece.Status = shared.Pending
		piece.ReceivedBlocks = 0
		piece.RequestedBlocks = 0

		m.removeActivePiece(piece.Index)

		return false
	}

	piece.Status = shared.Verified
	return true
}
