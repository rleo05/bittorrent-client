package tracker

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	failureReason  = "failure reason"
	warningMessage = "warning message"
	interval       = "interval"
	minInterval    = "min interval"
	trackerID      = "tracker id"
	complete       = "complete"
	incomplete     = "incomplete"
	peers          = "peers"
	peers6         = "peers6"

	defaultInterval = 1800
)

func ParseResponse(data map[string]any) (*Response, error) {
	if failureBytes, ok := data[failureReason].([]byte); ok {
		return nil, fmt.Errorf("tracker error: %s", string(failureBytes))
	}

	resp := &Response{}

	if warningBytes, ok := data[warningMessage].([]byte); ok {
		resp.warningMessage = string(warningBytes)
	}

	intervalVal, ok := data[interval].(int64)
	if !ok || intervalVal <= 0 {
		resp.interval = defaultInterval
	} else {
		resp.interval = uint32(intervalVal)
	}

	if minIntervalVal, ok := data[minInterval].(int64); ok && minIntervalVal > 0 {
		resp.minInterval = uint32(minIntervalVal)
	}

	if trackerIDBytes, ok := data[trackerID].([]byte); ok {
		resp.trackerID = string(trackerIDBytes)
	}

	if completeVal, ok := data[complete].(int64); ok {
		resp.complete = uint32(completeVal)
	}

	if incompleteVal, ok := data[incomplete].(int64); ok {
		resp.incomplete = uint32(incompleteVal)
	}

	peersRaw, ok := data[peers]
	if !ok {
		return nil, fmt.Errorf("missing peers field")
	}

	peersList, err := parsePeers(peersRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing peers: %w", err)
	}
	resp.Peers = peersList

	if peers6Raw, ok := data[peers6].([]byte); ok {
		peers6List, err := parseCompactPeers6(peers6Raw)
		if err != nil {
			return nil, fmt.Errorf("parsing peers6: %w", err)
		}
		resp.Peers = append(resp.Peers, peers6List...)
	}

	return resp, nil
}

func parsePeers(raw any) ([]types.PeerAddress, error) {
	switch v := raw.(type) {
	case []byte:
		return parseCompactPeers(v)
	case []any:
		return parseDictPeers(v)
	default:
		return nil, fmt.Errorf("unexpected peers type")
	}
}

func parseCompactPeers(data []byte) ([]types.PeerAddress, error) {
	if len(data)%6 != 0 {
		return nil, fmt.Errorf("compact peers length is not a multiple of 6")
	}

	peersList := make([]types.PeerAddress, 0, len(data)/6)
	for i := 0; i < len(data); i += 6 {
		ip := net.IP(data[i : i+4])
		port := binary.BigEndian.Uint16(data[i+4 : i+6])
		peersList = append(peersList, types.PeerAddress{IP: ip, Port: port})
	}

	return peersList, nil
}

func parseCompactPeers6(data []byte) ([]types.PeerAddress, error) {
	if len(data)%18 != 0 {
		return nil, fmt.Errorf("compact peers6 length is not a multiple of 18")
	}

	peersList := make([]types.PeerAddress, 0, len(data)/18)
	for i := 0; i < len(data); i += 18 {
		ip := net.IP(data[i : i+16])
		port := binary.BigEndian.Uint16(data[i+16 : i+18])
		peersList = append(peersList, types.PeerAddress{IP: ip, Port: port})
	}

	return peersList, nil
}

func parseDictPeers(list []any) ([]types.PeerAddress, error) {
	peersList := make([]types.PeerAddress, 0, len(list))

	for _, entry := range list {
		dict, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		ipBytes, ok := dict["ip"].([]byte)
		if !ok {
			continue
		}
		ip := net.ParseIP(string(ipBytes))
		if ip == nil {
			continue
		}

		portRaw, ok := dict["port"].(int64)
		if !ok || portRaw < 0 || portRaw > 65535 {
			continue
		}

		port := uint16(portRaw)

		peersList = append(peersList, types.PeerAddress{
			IP:   ip,
			Port: port,
		})
	}

	return peersList, nil
}