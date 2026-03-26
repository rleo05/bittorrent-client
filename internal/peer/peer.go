package peer

type Peer struct {
	ip string
	port int
}

type Manager struct {
}

func NewManager() *Manager {
	return &Manager{}
}