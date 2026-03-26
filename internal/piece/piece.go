package piece

type BlockRequest struct {
	pieceIndex int
	offset int
	length int64
}

type BlockResponse struct {
	pieceIndex int
	offset int
	data []byte
	peerID string
}

type Manager struct {
}

func NewManager() *Manager {
	return &Manager{}
}