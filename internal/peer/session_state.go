package peer

import (
	"context"
	"encoding/binary"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

func (s *PeerSession) stateMachine(ctx context.Context) {
	defer s.releaseInFlightRequests()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.msgChan:
			if err := s.handleMessage(msg); err != nil {
				s.logSessionError(err)
				return
			}

			s.seenFirstMessage = true
		case command := <-s.commandChan:
			if err := s.handleCommand(command); err != nil {
				s.logSessionError(err)
				return
			}
		}
	}
}

func (s *PeerSession) handleMessage(msg PeerMessage) error {
	s.logPeerf("message received: type=%s", msg.messageStatus)

	switch msg.messageStatus {
	case Choke:
		return s.onChoke(msg)
	case Unchoke:
		return s.onUnchoke(msg)
	case Interested:
		return nil
	case NotInterested:
		return nil
	case Have:
		return s.onHave(msg)
	case Bitfield:
		return s.onBitfield(msg)
	case Request:
		return nil
	case Piece:
		return s.onPiece(msg)
	case Cancel:
		return nil
	default:
		return s.peerErrorf("unknown message status=%d", msg.messageStatus)
	}
}

func (s *PeerSession) handleCommand(command sessionCommand) error {
	switch command.commandType {
	case sendHave:
		s.enqueueHave(command.pieceIndex)
		return nil
	default:
		return s.peerErrorf("unknown command type=%d", command.commandType)
	}
}

func (s *PeerSession) enqueueHave(pieceIndex int) {
	payload := make([]byte, 9)
	binary.BigEndian.PutUint32(payload[0:4], 5)
	payload[4] = byte(Have)
	binary.BigEndian.PutUint32(payload[5:9], uint32(pieceIndex))

	s.outboundChan <- payload
}

func (s *PeerSession) enqueueInterested() {
	payload := make([]byte, 5)
	binary.BigEndian.PutUint32(payload[0:4], 1)
	payload[4] = byte(Interested)

	s.outboundChan <- payload
}

func (s *PeerSession) enqueueNotInterested() {
	payload := make([]byte, 5)
	binary.BigEndian.PutUint32(payload[0:4], 1)
	payload[4] = byte(NotInterested)

	s.outboundChan <- payload
}

func (s *PeerSession) enqueueRequest(b shared.BlockRequest) {
	payload := make([]byte, 17)
	binary.BigEndian.PutUint32(payload[0:4], 13)
	payload[4] = byte(Request)
	binary.BigEndian.PutUint32(payload[5:9], uint32(b.PieceIndex))
	binary.BigEndian.PutUint32(payload[9:13], uint32(b.Offset))
	binary.BigEndian.PutUint32(payload[13:17], b.Length)

	s.outboundChan <- payload
}

func (s *PeerSession) onChoke(msg PeerMessage) error {
	s.peerChoking = true

	if len(s.inFlightRequests) > 0 {
		s.releaseInFlightRequests()
	}

	return nil
}

func (s *PeerSession) onUnchoke(msg PeerMessage) error {
	s.peerChoking = false
	s.fillRequests()
	return nil
}

func (s *PeerSession) onHave(msg PeerMessage) error {
	if len(msg.payload) != 4 {
		return s.peerErrorf("invalid have message length=%d", len(msg.payload))
	}

	if s.bitfield == nil {
		s.bitfield = s.pieceManager.GetEmptyBitfield()
	}

	pieceIndex := binary.BigEndian.Uint32(msg.payload)

	if pieceIndex >= uint32(s.pieceManager.TotalPieces) {
		return s.peerErrorf("invalid have piece index=%d", pieceIndex)
	}

	byteIndex := int(pieceIndex / 8)
	bitIndex := int(pieceIndex % 8)

	s.bitfield[byteIndex] |= byte(1 << (7 - bitIndex))

	s.toggleInterest()
	s.fillRequests()
	return nil
}

func (s *PeerSession) onBitfield(msg PeerMessage) error {
	if s.seenFirstMessage {
		return s.peerErrorf("bitfield received after first message")
	}

	if !s.pieceManager.IsPeerBitfieldValid(msg.payload) {
		return s.peerErrorf("invalid bitfield")
	}

	s.bitfield = msg.payload

	s.toggleInterest()
	s.fillRequests()
	return nil
}

func (s *PeerSession) onPiece(msg PeerMessage) error {
	if len(msg.payload) < 9 {
		return s.peerErrorf("invalid piece length")
	}

	pieceIndex := binary.BigEndian.Uint32(msg.payload[0:4])
	begin := binary.BigEndian.Uint32(msg.payload[4:8])
	blockData := msg.payload[8:]
	key := shared.InFlightKey{PieceIndex: int(pieceIndex), Offset: int(begin)}

	err := s.pieceManager.HandleReceivedBlock(pieceIndex, begin, blockData)
	if err != nil {
		return s.peerErrorf(err.Error())
	}

	delete(s.inFlightRequests, key)
	s.pieceManager.ReleaseInFlightRequest(key)

	s.fillRequests()
	return nil
}

func (s *PeerSession) toggleInterest() {
	hasInterest := s.pieceManager.HasInterestingPieces(s.bitfield)

	if !s.amInterested && hasInterest {
		s.amInterested = true
		s.enqueueInterested()
		return
	}

	if s.amInterested && !hasInterest {
		s.amInterested = false
		s.enqueueNotInterested()
	}
}

func (s *PeerSession) fillRequests() {
	if !s.amInterested || s.peerChoking {
		return
	}

	availableSlots := maxInFlightRequests - len(s.inFlightRequests)
	if availableSlots <= 0 {
		return
	}

	blockRequets := s.pieceManager.FillPeerInFlightRequests(s.bitfield, availableSlots, s.sessionID)

	for _, b := range blockRequets {
		key := shared.InFlightKey{PieceIndex: b.PieceIndex, Offset: b.Offset}
		s.inFlightRequests[key] = int(b.Length)
		s.enqueueRequest(b)
	}
}

func (s *PeerSession) releaseInFlightRequests() {
	for k := range s.inFlightRequests {
		s.pieceManager.ReleaseInFlightRequest(k)
		delete(s.inFlightRequests, k)
	}
}
