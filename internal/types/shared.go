package types

import (
	"net"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
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

func NewPeerAddress(ip net.IP, port uint16) PeerAddress {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = append(net.IP(nil), ipv4...)
	} else if ipv6 := ip.To16(); ipv6 != nil {
		ip = append(net.IP(nil), ipv6...)
	} else {
		ip = append(net.IP(nil), ip...)
	}

	return PeerAddress{
		IP:   ip,
		Port: port,
	}
}

func (p PeerAddress) String() string {
	return net.JoinHostPort(p.IP.String(), strconv.FormatUint(uint64(p.Port), 10))
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

type Tracker struct {
	Url          *url.URL
	Tier         int
	Fails        int
	Interval     time.Duration
	MinInterval  time.Duration
	NextAnnounce time.Time
}

type PieceStatus int

const (
	Pending PieceStatus = iota
	Complete
	Verified
	Writing
	Stored
)

type PieceState struct {
	Index int
	Length int
	Status PieceStatus
	Hash [20]byte
	Data []byte
	Blocks []Block
	ReceivedBlocks int
	RequestedBlocks int
}

type BlockStatus int

const (
	Missing BlockStatus = iota
	Requested
	Received
	
)

type Block struct {
	PieceIndex int
	Offset int
	Size int
	Status BlockStatus
	RequestedBy PeerAddress 
}

type BlockKey struct {
	PieceIndex int
	Offset int
}