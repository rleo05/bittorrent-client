package tracker

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/bencode"
	"github.com/rleo05/bittorrent-client/internal/types"
)

type Config struct {
	InfoHash     [20]byte
	PeerID       [20]byte
	Announce     *url.URL
	AnnounceList [][]*url.URL
	PeerChan     chan types.PeerAddress
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

type Response struct {
	Peers          []types.PeerAddress
	interval       uint32
	minInterval    uint32
	trackerID      string
	complete       uint32
	incomplete     uint32
	warningMessage string
}

type Manager struct {
	*types.Stats
	Config
	httpClient *http.Client
}

func NewManager(stats *types.Stats, cfg Config) *Manager {
	return &Manager{
		Stats:  stats,
		Config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

var (
	UDPEvents = map[string]uint32{
		"":          0,
		"completed": 1,
		"started":   2,
		"stopped":   3,
	}

	UDPKeys = make(map[string]uint32)
)

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	event := "started"

	for {
		request := &AnnounceRequest{
			Url:        m.AnnounceList[2][0],
			InfoHash:   m.InfoHash,
			PeerID:     m.PeerID,
			Port:       m.Port,
			Uploaded:   m.Uploaded.Load(),
			Downloaded: m.Downloaded.Load(),
			Left:       m.Left.Load(),
			Event:      event,
		}

		var resp *Response
		var err error

		switch m.AnnounceList[1][0].Scheme {
		case "udp":
			resp, err = m.handleUdpRequest(request, ctx)
		case "http", "https":
			resp, err = m.handleHttpRequest(request, ctx)
		}

		if err != nil {
			log.Printf("error requesting tracker: %+v", err)

			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		event = ""

		for _, peer := range resp.Peers {
			select {
			case m.PeerChan <- peer:
			default:
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(resp.interval) * time.Second):
			continue
		}
	}
}

func (m *Manager) handleHttpRequest(request *AnnounceRequest, ctx context.Context) (*Response, error) {
	params := createHttpQueryParam(request)

	request.Url.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, request.Url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating http request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing http request: %w", err)
	}
	defer resp.Body.Close()

	rawBencode, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading http response: %w", err)
	}

	parsedBencode, _, err := bencode.Parse(rawBencode)
	if err != nil {
		return nil, fmt.Errorf("error parsing http bencode: %w", err)
	}

	bencodeMap, ok := parsedBencode.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: not a dictionary")
	}

	return ParseResponse(bencodeMap)
}

func createHttpQueryParam(request *AnnounceRequest) url.Values {
	params := url.Values{}

	params.Add("info_hash", string(request.InfoHash[:]))
	params.Add("peer_id", string(request.PeerID[:]))
	params.Add("port", strconv.FormatUint(uint64(request.Port), 10))
	params.Add("left", strconv.FormatUint(request.Left, 10))
	params.Add("downloaded", strconv.FormatUint(request.Downloaded, 10))
	params.Add("uploaded", strconv.FormatUint(request.Uploaded, 10))
	params.Add("compact", "1")

	if request.Event != "" {
		params.Add("event", request.Event)
	}

	return params
}

func (m *Manager) handleUdpRequest(request *AnnounceRequest, ctx context.Context) (*Response, error) {
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
	connectionID := binary.BigEndian.Uint64(readBuf[8:16])

	if action != 0 || transactionID != uint32(connectTransactionId) {
		return nil, fmt.Errorf("invalid udp response")
	}

	key, ok := UDPKeys[request.Url.Host]
	if !ok {
		key = rand.Uint32()
		UDPKeys[request.Url.Host] = key
	}

	announceTransactionId := rand.Uint32()
	announceRequest := &UDPAnnouncePacket{
		ConnectionID: connectionID,
		Action: 1,
		TransactionID: announceTransactionId,
		InfoHash: request.InfoHash,
		PeerID: request.PeerID,
		Downloaded: request.Downloaded,
		Left: request.Left,
		Uploaded: request.Uploaded,
		Event: UDPEvents[request.Event],
		IpAddress: 0,
		Key: key,
		NumWant: 50,
		Port: request.Port,
	}

	buf = createUdpAnnouncePacket(announceRequest)

	_, err = conn.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("error writing to udp announce: %w", err)
	}

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
	interval := binary.BigEndian.Uint32(readBuf[8:12])
	_ = binary.BigEndian.Uint32(readBuf[12:16]) // leechers
	_ = binary.BigEndian.Uint32(readBuf[16:20]) // seeders
	rawPeers := readBuf[20:n]

	if transactionID != announceTransactionId {
		return nil, fmt.Errorf("invalid announce transactionID")
	}

	if action != 1 {
		return nil, fmt.Errorf("invalid action: expected 1, got %d", action)
	}

	response := &Response{interval: interval}

	var peers []types.PeerAddress
	if isIpv6 {
		if len(rawPeers) % 18 != 0 {
			return response, nil
		}
		peers, err = parseIpv6Peers(rawPeers)
	} else {
		if len(rawPeers) % 6 != 0 {
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
	peers := make([]types.PeerAddress, 0, len(rawPeers) % 6)

	for i := 0; i < len(rawPeers); i+=6 {
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