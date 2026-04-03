package tracker

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"time"

	"github.com/rleo05/bittorrent-client/internal/types"
)

func (m *Manager) handleUdpRequest(request *AnnounceRequest, ctx context.Context) (*TrackerResponse, error) {
	connectTransactionId := rand.Uint32()
	connectionRequest := &UDPConnectPacket{
		ProtocolID:    0x41727101980,
		Action:        0,
		TransactionID: connectTransactionId,
	}

	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:], connectionRequest.ProtocolID)
	binary.BigEndian.PutUint32(buf[8:], connectionRequest.Action)
	binary.BigEndian.PutUint32(buf[12:], connectionRequest.TransactionID)

	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(ctx, "udp", request.Url.Host)
	if err != nil {
		return nil, fmt.Errorf("error connecting to udp address: %w", err)
	}
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() {
		conn.SetReadDeadline(time.Now())
	})
	defer stop()

	udpAddr := conn.LocalAddr().(*net.UDPAddr)
	isIpv6 := udpAddr.IP.To4() == nil

	_, err = conn.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("error writing to udp connection: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	readBuf := make([]byte, 16)
	n, err := conn.Read(readBuf)
	if err != nil {
		return nil, fmt.Errorf("error reading udp connect packet: %w", err)
	}

	if n != 16 {
		return nil, fmt.Errorf("invalid udp response: expected 16 bytes, got %d", n)
	}

	action := binary.BigEndian.Uint32(readBuf[0:4])
	transactionID := binary.BigEndian.Uint32(readBuf[4:8])

	if action == 3 {
		return nil, fmt.Errorf("tracker error: %s", string(readBuf[8:n]))
	}

	if action != 0 {
		return nil, fmt.Errorf("invalid udp connect action: %d", action)
	}

	if transactionID != connectTransactionId {
		return nil, fmt.Errorf("invalid udp connect transaction id")
	}

	connectionID := binary.BigEndian.Uint64(readBuf[8:16])

	key := m.getUDPKey(request.Url.Host)

	announceTransactionId := rand.Uint32()
	announceRequest := &UDPAnnouncePacket{
		ConnectionID:  connectionID,
		Action:        1,
		TransactionID: announceTransactionId,
		InfoHash:      request.InfoHash,
		PeerID:        request.PeerID,
		Downloaded:    request.Downloaded,
		Left:          request.Left,
		Uploaded:      request.Uploaded,
		Event:         m.udpEvents[request.Event],
		IpAddress:     0,
		Key:           key,
		NumWant:       -1,
		Port:          request.Port,
	}

	buf = createUdpAnnouncePacket(announceRequest)

	_, err = conn.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("error writing to udp announce: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	readBuf = make([]byte, 1500)
	n, err = conn.Read(readBuf)
	if err != nil {
		return nil, fmt.Errorf("error reading udp announce packet: %w", err)
	}

	if n < 20 {
		return nil, fmt.Errorf("malformed udp announce packet")
	}

	action = binary.BigEndian.Uint32(readBuf[0:4])
	transactionID = binary.BigEndian.Uint32(readBuf[4:8])

	if action == 3 {
		return nil, fmt.Errorf("%s", string(readBuf[8:n]))
	}

	if action != 1 {
		return nil, fmt.Errorf("invalid action: expected 1, got %d", action)
	}

	interval := binary.BigEndian.Uint32(readBuf[8:12])
	_ = binary.BigEndian.Uint32(readBuf[12:16]) // leechers
	_ = binary.BigEndian.Uint32(readBuf[16:20]) // seeders
	rawPeers := readBuf[20:n]

	if transactionID != announceTransactionId {
		return nil, fmt.Errorf("invalid announce transactionID")
	}

	response := &TrackerResponse{interval: interval}

	var peers []types.PeerAddress
	if isIpv6 {
		if len(rawPeers)%18 != 0 {
			return response, nil
		}
		peers, err = parseIpv6Peers(rawPeers)
	} else {
		if len(rawPeers)%6 != 0 {
			return response, nil
		}
		peers, err = parseIpv4Peers(rawPeers)
	}

	if err != nil {
		log.Println("error parsing peers")
	}

	response.Peers = peers

	return response, nil
}

func parseIpv6Peers(rawPeers []byte) ([]types.PeerAddress, error) {
	peers := make([]types.PeerAddress, 0, len(rawPeers)/18)

	for i := 0; i < len(rawPeers); i += 18 {
		ipBytes := rawPeers[i : i+16]
		port := binary.BigEndian.Uint16(rawPeers[i+16 : i+18])

		isZeroIp := binary.BigEndian.Uint64(ipBytes[:8]) == 0 && binary.BigEndian.Uint64(ipBytes[8:16]) == 0
		if isZeroIp || port == 0 {
			continue
		}

		ip := net.IP(ipBytes)
		peers = append(peers, types.PeerAddress{IP: ip, Port: port})
	}

	return peers, nil
}

func parseIpv4Peers(rawPeers []byte) ([]types.PeerAddress, error) {
	peers := make([]types.PeerAddress, 0, len(rawPeers)/6)

	for i := 0; i < len(rawPeers); i += 6 {
		ipBytes := rawPeers[i : i+4]
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i+6])

		if binary.BigEndian.Uint32(ipBytes) == 0 || port == 0 {
			continue
		}

		ip := net.IP(ipBytes)
		peers = append(peers, types.PeerAddress{IP: ip, Port: port})
	}

	return peers, nil
}

func createUdpAnnouncePacket(announceRequest *UDPAnnouncePacket) []byte {
	buf := make([]byte, 98)

	binary.BigEndian.PutUint64(buf[0:8], announceRequest.ConnectionID)
	binary.BigEndian.PutUint32(buf[8:12], announceRequest.Action)
	binary.BigEndian.PutUint32(buf[12:16], announceRequest.TransactionID)

	copy(buf[16:36], announceRequest.InfoHash[:])
	copy(buf[36:56], announceRequest.PeerID[:])

	binary.BigEndian.PutUint64(buf[56:64], announceRequest.Downloaded)
	binary.BigEndian.PutUint64(buf[64:72], announceRequest.Left)
	binary.BigEndian.PutUint64(buf[72:80], announceRequest.Uploaded)

	binary.BigEndian.PutUint32(buf[80:84], announceRequest.Event)
	binary.BigEndian.PutUint32(buf[84:88], announceRequest.IpAddress)
	binary.BigEndian.PutUint32(buf[88:92], announceRequest.Key)
	binary.BigEndian.PutUint32(buf[92:96], uint32(announceRequest.NumWant))
	binary.BigEndian.PutUint16(buf[96:98], announceRequest.Port)

	return buf
}
