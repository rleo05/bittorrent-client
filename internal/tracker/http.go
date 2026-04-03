package tracker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/rleo05/bittorrent-client/internal/bencode"
)

func handleHttpRequest(httpClient *http.Client, request *AnnounceRequest, ctx context.Context) (*TrackerResponse, error) {
	params := createHttpQueryParam(request)
	url := *request.Url
	url.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating http request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker returned status %d", resp.StatusCode)
	}

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
