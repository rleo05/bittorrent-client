package torrent

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/rleo05/bittorrent-client/internal/bencode"
	"github.com/rleo05/bittorrent-client/internal/types"
)

const (
	info         = "info"
	announce     = "announce"
	creationDate = "creation date"
	comment      = "comment"
	createdBy    = "created by"
	encoding     = "encoding"
	announceList = "announce-list"
	httpseeds    = "httpseeds"
	urlList      = "url-list"
	nodes        = "nodes"
	pieceLength  = "piece length"
	pieces       = "pieces"
	name         = "name"
	private      = "private"
	source       = "source"
	length       = "length"
	md5sum       = "md5sum"
	files        = "files"
	path         = "path"
	nameUTF8     = "name.utf-8"
	pathUTF8     = "path.utf-8"
	attr         = "attr"
)

func ParseTorrent(raw []byte) (*Torrent, error) {
	parsed, rawInfo, err := bencode.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bencode parse: %w", err)
	}

	data, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid .torrent file: root is not a dictionary")
	}

	infoHash := sha1.Sum(rawInfo)

	return parseTorrent(data, infoHash)
}

func parseTorrent(data map[string]any, infoHash [20]byte) (*Torrent, error) {
	torrent := &Torrent{}

	if data[announce] == nil && data[announceList] == nil {
		return nil, fmt.Errorf("missing announce and announce-list")
	}

	if announceBytes, ok := data[announce].([]byte); ok {
		u, err := url.ParseRequestURI(string(announceBytes))
		if err == nil {
			torrent.Announce = u
		} else if data[announceList] == nil {
			return nil, fmt.Errorf("invalid announce URL %q: %w", string(announceBytes), err)
		}
	}

	if announceListRaw, ok := data[announceList].([]any); ok {
		list := make([][]*url.URL, 0, len(announceListRaw))
		for _, tierRaw := range announceListRaw {
			tierList, ok := tierRaw.([]any)
			if !ok {
				continue
			}
			tier := make([]*url.URL, 0, len(tierList))
			for _, trackerRaw := range tierList {
				if trackerBytes, ok := trackerRaw.([]byte); ok {
					u, err := url.ParseRequestURI(string(trackerBytes))
					if err == nil {
						tier = append(tier, u)
					}
				}
			}
			if len(tier) > 0 {
				list = append(list, tier)
			}
		}
		if len(list) == 0 && torrent.Announce == nil {
			return nil, fmt.Errorf("announce-list contains no valid URLs and announce is missing")
		}
		if len(list) > 0 {
			torrent.AnnounceList = list
		}
	}

	if commentBytes, ok := data[comment].([]byte); ok {
		s := string(commentBytes)
		torrent.Comment = &s
	}

	if creationDateVal, ok := data[creationDate].(int64); ok {
		t := time.Unix(creationDateVal, 0)
		torrent.CreationDate = &t
	}

	if createdByBytes, ok := data[createdBy].([]byte); ok {
		s := string(createdByBytes)
		torrent.CreatedBy = &s
	}

	if encodingBytes, ok := data[encoding].([]byte); ok {
		s := string(encodingBytes)
		torrent.Encoding = &s
	}

	if httpSeedsRaw, ok := data[httpseeds].([]any); ok {
		seeds := make([]string, 0, len(httpSeedsRaw))
		for _, seedRaw := range httpSeedsRaw {
			if seedBytes, ok := seedRaw.([]byte); ok {
				seeds = append(seeds, string(seedBytes))
			}
		}
		if len(seeds) > 0 {
			torrent.HttpSeeds = &seeds
		}
	}

	if urlListRaw, ok := data[urlList].([]any); ok {
		urls := make([]string, 0, len(urlListRaw))
		for _, urlRaw := range urlListRaw {
			if urlBytes, ok := urlRaw.([]byte); ok {
				urls = append(urls, string(urlBytes))
			}
		}
		if len(urls) > 0 {
			torrent.UrlList = &urls
		}
	} else if urlBytes, ok := data[urlList].([]byte); ok {
		urls := []string{string(urlBytes)}
		torrent.UrlList = &urls
	}

	if nodesRaw, ok := data[nodes].([]any); ok {
		nodesList := make([]Node, 0, len(nodesRaw))
		for _, nodeRaw := range nodesRaw {
			if node, ok := nodeRaw.([]any); ok {
				if len(node) != 2 {
					continue
				}
				host, ok := node[0].([]byte)
				if !ok {
					continue
				}
				port, ok := node[1].(int64)
				if !ok {
					continue
				}
				if port < 0 || port > 65535 {
					continue
				}
				nodesList = append(nodesList, Node{
					Host: string(host),
					Port: int(port),
				})
			}
		}
		if len(nodesList) > 0 {
			torrent.Nodes = nodesList
		}
	}

	infoRaw, ok := data[info].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing or invalid info")
	}

	infoData, err := parseInfo(infoRaw)
	if err != nil {
		return nil, fmt.Errorf("decoding info: %w", err)
	}
	torrent.Info = *infoData
	torrent.InfoHash = infoHash

	if infoData.Length != nil {
		torrent.TotalLength = *infoData.Length
	} else {
		var totalLength uint64
		for _, v := range *torrent.Info.Files {
			totalLength += v.Length
		}
		torrent.TotalLength = totalLength
	}

	return torrent, nil
}

func parseInfo(infoMap map[string]any) (*Info, error) {
	result := &Info{}

	nameBytes, ok := infoMap[name].([]byte)
	if !ok || len(nameBytes) == 0 {
		return nil, fmt.Errorf("missing or invalid info.name")
	}
	result.Name = filepath.Clean(strings.TrimLeft(string(nameBytes), "/"))

	if nameUTF8Bytes, ok := infoMap[nameUTF8].([]byte); ok {
		s := filepath.Clean(strings.TrimLeft(string(nameUTF8Bytes), "/"))
		result.NameUTF8 = &s
	}

	pieceLen, ok := infoMap[pieceLength].(int64)
	if !ok || pieceLen <= 0 {
		return nil, fmt.Errorf("missing or invalid info.piece length")
	}
	result.PieceLength = uint64(pieceLen)

	piecesBytes, ok := infoMap[pieces].([]byte)
	if !ok || len(piecesBytes) == 0 {
		return nil, fmt.Errorf("missing or invalid info.pieces")
	}
	if len(piecesBytes)%20 != 0 {
		return nil, fmt.Errorf("invalid info.pieces")
	}
	result.Pieces = piecesBytes

	if privateVal, ok := infoMap[private].(int64); ok {
		if privateVal != 0 && privateVal != 1 {
			return nil, fmt.Errorf("invalid info.private")
		}
		privateBool := privateVal == 1
		result.Private = &privateBool
	}

	if sourceBytes, ok := infoMap[source].([]byte); ok {
		s := string(sourceBytes)
		result.Source = &s
	}

	if md5sumBytes, ok := infoMap[md5sum].([]byte); ok {
		s := string(md5sumBytes)
		result.Md5Sum = &s
	}

	var lengthVal *uint64
	if l, ok := infoMap[length].(int64); ok {
		ul := uint64(l)
		lengthVal = &ul
		result.Length = lengthVal
	}

	var filesRaw []any
	if f, ok := infoMap[files].([]any); ok {
		filesRaw = f
	}

	if lengthVal == nil && filesRaw == nil {
		return nil, fmt.Errorf("info must contain 'length' or 'files'")
	}

	if lengthVal != nil && filesRaw != nil {
		return nil, fmt.Errorf("info must have either 'length' or 'files', not both")
	}

	filesList, err := parseFiles(filesRaw)
	if err != nil {
		return nil, err
	}
	result.Files = &filesList

	return result, nil
}

func parseFiles(filesRaw []any) ([]types.File, error) {
	filesList := make([]types.File, 0, len(filesRaw))

	for i, fileRaw := range filesRaw {
		fileDict, ok := fileRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("files[%d]: invalid file entry", i)
		}

		lengthVal, ok := fileDict[length].(int64)
		if !ok {
			return nil, fmt.Errorf("files[%d]: missing or invalid length", i)
		}

		pathRaw, ok := fileDict[path].([]any)
		if !ok || len(pathRaw) == 0 {
			return nil, fmt.Errorf("files[%d]: missing or invalid path", i)
		}

		pathSegments := make([]string, 0, len(pathRaw))
		for _, segment := range pathRaw {
			segBytes, ok := segment.([]byte)
			if !ok {
				return nil, fmt.Errorf("files[%d]: invalid path segment", i)
			}
			pathSegments = append(pathSegments, filepath.Clean(string(segBytes)))
		}

		file := types.File{
			Length: uint64(lengthVal),
			Path:   pathSegments,
		}

		if pathUTF8Raw, ok := fileDict[pathUTF8].([]any); ok {
			pathUTF8Segments := make([]string, 0, len(pathUTF8Raw))
			for _, segment := range pathUTF8Raw {
				if segBytes, ok := segment.([]byte); ok {
					pathUTF8Segments = append(pathUTF8Segments, string(segBytes))
				}
			}
			if len(pathUTF8Segments) > 0 {
				file.PathUTF8 = &pathUTF8Segments
			}
		}

		if md5sumBytes, ok := fileDict[md5sum].([]byte); ok {
			s := string(md5sumBytes)
			file.Md5Sum = &s
		}

		if attrBytes, ok := fileDict[attr].([]byte); ok {
			s := string(attrBytes)
			file.Attr = &s
		}

		filesList = append(filesList, file)
	}

	return filesList, nil
}
