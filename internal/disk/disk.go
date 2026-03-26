package disk

type DiskWrite struct {
	pieceIndex int
	data []byte
}

type Manager struct {
}

func NewManager() *Manager {
	return &Manager{}
}