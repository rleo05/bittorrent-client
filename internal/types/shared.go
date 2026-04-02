package types

import (
	"net"
	"sync/atomic"
)

type Stats struct {
	Downloaded atomic.Uint64
	Uploaded   atomic.Uint64
	Left       atomic.Uint64
}

type PeerAddress struct {
	IP   net.IP
	Port uint16
}

type BlockRequest struct {
	PieceIndex int
	Offset     int
	Length     uint32
}

type BlockResponse struct {
	PieceIndex int
	Offset     int
	Data       []byte
	PeerID     string
}

type DiskWrite struct {
	PieceIndex int
	Data       []byte
}

type File struct {
	Length uint64

	Path     []string
	PathUTF8 *[]string

	Md5Sum *string
	Attr   *string
}
