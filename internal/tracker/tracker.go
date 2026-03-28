package tracker

import (
	"context"
	"fmt"
	"io"
	"log"
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
	Port         int
}

type Request struct {
	Url 	   *url.URL
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string
}

type Response struct {
	Peers          []types.PeerAddress
	Peers6         []types.PeerAddress
	interval       int
	minInterval    int
	trackerID      string
	complete       int
	incomplete     int
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

const (
	defaultWaitTime = 30
	maxWaitTime = 30 * time.Minute
)

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	event := "started"
	var waitTime time.Duration
	totalFailedAttempts := 0

	for {
		request := &Request{
			Url: 		m.Announce,
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

		switch m.Announce.Scheme {
		case "udp":
			resp, err = m.handleUdpRequest(request)
		case "http", "https":
			resp, err = m.handleHttpRequest(request)
		}

		if err != nil {
			log.Printf("error requesting tracker: %v", err)
			
			multiplier := 1 << totalFailedAttempts
            calculatedWait := time.Duration(defaultWaitTime * multiplier) * time.Second

            if calculatedWait < maxWaitTime { 
                waitTime = calculatedWait
                totalFailedAttempts++ 
            } else {
                waitTime = maxWaitTime
            }
		} else {
			event = ""
			waitTime = time.Duration(resp.interval) * time.Second
			totalFailedAttempts = 0
		}

		select {
		case <- ctx.Done():
			return
		case <- time.After(waitTime):
			continue
		}
	}
}

func (m *Manager) handleHttpRequest(request *Request) (*Response, error) {
	params := createHttpQueryParam(request)

	request.Url.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, request.Url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating http request: %v", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing http request: %v", err)
	}
	defer resp.Body.Close()

	rawBencode, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading http response: %v", err)
	}

	parsedBencode, _, err := bencode.Parse(rawBencode)
	if err != nil {
		return nil, fmt.Errorf("error parsing http bencode: %v", err)
	}

	bencodeMap, ok := parsedBencode.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: not a dictionary")
	}

	return ParseResponse(bencodeMap)
}

func createHttpQueryParam(request *Request) url.Values{
	params := url.Values{}

	params.Add("info_hash", string(request.InfoHash[:]))
	params.Add("peer_id", string(request.PeerID[:]))
	params.Add("port", strconv.Itoa(request.Port))
	params.Add("left", strconv.FormatInt(request.Left, 10))
	params.Add("downloaded", strconv.FormatInt(request.Downloaded, 10))
	params.Add("uploaded", strconv.FormatInt(request.Uploaded, 10))
	params.Add("compact", "1")

	if request.Event != "" {
		params.Add("event", request.Event)
	}

	return params
}

func (m *Manager) handleUdpRequest(request *Request) (*Response, error) {
	return nil, nil
}