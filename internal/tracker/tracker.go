package tracker

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rleo05/bittorrent-client/internal/types"
)

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
	minDelay = 30 * time.Second
	maxDelay = 1 * time.Hour
)

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
	trackerList := m.getTrackerList()

	for {
		var nextAnnounce time.Duration

		nextAnnounce, announced := m.runAnnounceCycle(ctx, trackerList, event)

		if announced {
			event = ""
		}

		if nextAnnounce == 0 {
			nextAnnounce = getNextAnnounceDelay(trackerList)
		}

		if nextAnnounce > maxDelay {
			nextAnnounce = maxDelay
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(nextAnnounce):
			continue
		}
	}
}

func (m *Manager) runAnnounceCycle(ctx context.Context, trackerList [][]*types.Tracker, event string) (time.Duration, bool) {
	request := m.buildAnnounceRequest(event)
	var nextAnnounce time.Duration

	// iterate over each tier in the trackerlist
	for _, v := range trackerList {
		// iterate over each tracker in the tier
		for j, t := range v {
			if !canAnnounce(t) {
				continue
			}

			request.Url = t.Url

			resp, err := m.handleAnnounceRequest(ctx, request)

			if err != nil {
				t.Fails++
				nextAnnounceDuration := backoff(t.Fails)
				t.NextAnnounce = time.Now().Add(nextAnnounceDuration)

				continue
			}

			t.Fails = 0
			t.MinInterval = time.Duration(resp.minInterval) * time.Second
			t.Interval = time.Duration(resp.interval) * time.Second

			nextAnnounceDuration := time.Duration(resp.interval) * time.Second
			t.NextAnnounce = time.Now().Add(nextAnnounceDuration)

			v[0], v[j] = v[j], v[0]

			for _, peer := range resp.Peers {
				select {
				case m.PeerChan <- peer:
				default:
				}
			}

			nextAnnounce = nextAnnounceDuration

			return nextAnnounce, true
		}
	}

	return nextAnnounce, false
}

func (m *Manager) handleAnnounceRequest(ctx context.Context, req *AnnounceRequest) (*TrackerResponse, error) {
	switch req.Url.Scheme {
	case "udp":
		return handleUdpRequest(req, ctx)
	case "http", "https":
		return handleHttpRequest(m.httpClient, req, ctx)
	default:
		return nil, fmt.Errorf("unsupported protocol")
	}
}

func (m *Manager) getTrackerList() [][]*types.Tracker {
	trackerList := [][]*types.Tracker{{m.Announce}}
	if len(m.AnnounceList) > 0 {
		trackerList = m.AnnounceList
	}
	return trackerList
}

func (m *Manager) buildAnnounceRequest(event string) *AnnounceRequest {
	return &AnnounceRequest{
		InfoHash:   m.InfoHash,
		PeerID:     m.PeerID,
		Port:       m.Port,
		Uploaded:   m.Uploaded.Load(),
		Downloaded: m.Downloaded.Load(),
		Left:       m.Left.Load(),
		Event:      event,
	}
}

func getNextAnnounceDelay(trackerList [][]*types.Tracker) time.Duration {
	earliestTime := trackerList[0][0].NextAnnounce

	for _, tier := range trackerList {
		for _, tracker := range tier {
			if tracker.NextAnnounce.Before(earliestTime) {
				earliestTime = tracker.NextAnnounce
			}
		}
	}

	nextAnnounce := time.Until(earliestTime)
	if nextAnnounce <= 0 {
		return 5 * time.Second
	}

	return nextAnnounce
}

func canAnnounce(t *types.Tracker) bool {
	return !time.Now().Before(t.NextAnnounce)
}

func backoff(fails int) time.Duration {
	if fails <= 0 {
		return minDelay
	}
	secs := 30 * (1 << (fails - 1))
	return time.Duration(secs) * time.Second
}
