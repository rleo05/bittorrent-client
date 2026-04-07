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

type PeerSession struct {
	conn         net.Conn
	address      string
	bitfield     []byte
	pieceManager *piece.Manager

	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	inFlightRequests map[types.InFlightKey]struct{}

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
	sessionCommandSendHave sessionCommandType = iota
)

type sessionCommand struct {
	commandType sessionCommandType
	pieceIndex  int
}
