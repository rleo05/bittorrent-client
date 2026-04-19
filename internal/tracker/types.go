package tracker

import (
	"net/url"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

type Config struct {
	InfoHash     [20]byte
	PeerID       [20]byte
	Announce     *shared.Tracker
	AnnounceList [][]*shared.Tracker
	PeerChan     chan shared.PeerAddress
	Completed    <-chan struct{}
	CompleteAck  chan<- struct{}
	Port         uint16
}

type AnnounceRequest struct {
	Url        *url.URL
	InfoHash   [20]byte
	PeerID     [20]byte
	Uploaded   uint64
	Downloaded uint64
	Left       uint64
	Event      string
	Port       uint16
}

type UDPConnectPacket struct {
	ProtocolID    uint64
	Action        uint32
	TransactionID uint32
}

type UDPAnnouncePacket struct {
	ConnectionID  uint64
	Action        uint32
	TransactionID uint32
	InfoHash      [20]byte
	PeerID        [20]byte
	Downloaded    uint64
	Left          uint64
	Uploaded      uint64
	Event         uint32
	IpAddress     uint32
	Key           uint32
	NumWant       int32
	Port          uint16
}

type TrackerResponse struct {
	Peers          []shared.PeerAddress
	interval       uint32
	minInterval    uint32
	trackerID      string
	complete       uint32
	incomplete     uint32
	warningMessage string
}
