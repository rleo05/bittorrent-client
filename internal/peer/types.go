package peer

import (
	"net"

	"github.com/rleo05/bittorrent-client/internal/piece"
	"github.com/rleo05/bittorrent-client/internal/types"
)

type MessageStatus int

const (
	Choke MessageStatus = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
)

func (m MessageStatus) String() string {
	switch m {
	case Choke:
		return "choke"
	case Unchoke:
		return "unchoke"
	case Interested:
		return "interested"
	case NotInterested:
		return "not_interested"
	case Have:
		return "have"
	case Bitfield:
		return "bitfield"
	case Request:
		return "request"
	case Piece:
		return "piece"
	case Cancel:
		return "cancel"
	default:
		return "unknown"
	}
}

type PeerSession struct {
	conn         net.Conn
	address      string
	bitfield     []byte
	pieceManager *piece.Manager

	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	seenFirstMessage bool

	inFlightRequests map[types.InFlightKey]int

	msgChan      chan PeerMessage
	commandChan  chan sessionCommand
	outboundChan chan []byte
}

type PeerMessage struct {
	messageStatus MessageStatus
	payload       []byte
}

type sessionCommandType int

const (
	sendHave sessionCommandType = iota
)

type sessionCommand struct {
	commandType sessionCommandType
	pieceIndex  int
}
