package types

import (
	"net"
	"sync/atomic"
)

type Stats struct {
	Downloaded atomic.Int64
	Uploaded   atomic.Int64
	Left       atomic.Int64
}

type PeerAddress struct {
	IP   net.IP
	Port int
}

type BlockRequest struct {
	PieceIndex int
	Offset     int
	Length     int64
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
	Length int64

	Path     []string
	PathUTF8 *[]string

	Md5Sum *string
	Attr   *string
}
