package torrent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	INFO          = "info"
	ANNOUNCE      = "announce"
	CREATION_DATE = "creation date"
	COMMENT       = "comment"
	CREATED_BY    = "created by"
	ENCODING      = "encoding"
	ANNOUNCE_LIST = "announce-list"
	HTTPSEEDS     = "httpseeds"
	URL_LIST      = "url-list"
	NODES         = "nodes"
	PIECE_LENGTH  = "piece length"
	PIECES        = "pieces"
	NAME          = "name"
	PRIVATE       = "private"
	SOURCE        = "source"
	LENGTH        = "length"
	MD5SUM        = "md5sum"
	FILES         = "files"
	PATH          = "path"
	NAME_UTF8     = "name.utf-8"
	PATH_UTF8     = "path.utf-8"
	ATTR          = "attr"
)

type Decoder struct {
	data     map[string]any
	infoHash [20]byte
}

func NewDecoder(data map[string]any, infoHash [20]byte) *Decoder {
	return &Decoder{data: data, infoHash: infoHash}
}

func (d *Decoder) DecodeTorrent() (*Torrent, error) {
	torrent := &Torrent{}

	if d.data[ANNOUNCE] == nil && d.data[ANNOUNCE_LIST] == nil {
		return nil, fmt.Errorf("missing announce and announce-list")
	}

	if announce, ok := d.data[ANNOUNCE].([]byte); ok {
		s := string(announce)
		torrent.Announce = &s
	}

	if announceListRaw, ok := d.data[ANNOUNCE_LIST].([]any); ok {
		list := make([][]string, 0, len(announceListRaw))
		for _, tierRaw := range announceListRaw {
			tierList, ok := tierRaw.([]any)
			if !ok {
				continue
			}
			subList := make([]string, 0, len(tierList))
			for _, trackerRaw := range tierList {
				if trackerBytes, ok := trackerRaw.([]byte); ok {
					subList = append(subList, string(trackerBytes))
				}
			}
			if len(subList) > 0 {
				list = append(list, subList)
			}
		}
		if len(list) > 0 {
			torrent.AnnounceList = list
		}
	}

	if comment, ok := d.data[COMMENT].([]byte); ok {
		s := string(comment)
		torrent.Comment = &s
	}

	if creationDate, ok := d.data[CREATION_DATE].(int64); ok {
		t := time.Unix(creationDate, 0)
		torrent.CreationDate = &t
	}

	if createdBy, ok := d.data[CREATED_BY].([]byte); ok {
		s := string(createdBy)
		torrent.CreatedBy = &s
	}

	if encoding, ok := d.data[ENCODING].([]byte); ok {
		s := string(encoding)
		torrent.Encoding = &s
	}

	if httpSeedsRaw, ok := d.data[HTTPSEEDS].([]any); ok {
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

	if urlListRaw, ok := d.data[URL_LIST].([]any); ok {
		urls := make([]string, 0, len(urlListRaw))
		for _, urlRaw := range urlListRaw {
			if urlBytes, ok := urlRaw.([]byte); ok {
				urls = append(urls, string(urlBytes))
			}
		}
		if len(urls) > 0 {
			torrent.UrlList = &urls
		}
	} else if urlBytes, ok := d.data[URL_LIST].([]byte); ok {
		s := string(urlBytes)
		urls := []string{s}
		torrent.UrlList = &urls
	}

	if nodesRaw, ok := d.data[NODES].([]any); ok {
		nodes := make([]Node, 0, len(nodesRaw))
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

				nodes = append(nodes, Node{
					Host: string(host),
					Port: int(port),
				})
			}
		}
		if len(nodes) > 0 {
			torrent.Nodes = nodes
		}
	}

	infoRaw, ok := d.data[INFO].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing or invalid info")
	}

	info, err := d.getInfo(infoRaw)
	if err != nil {
		return nil, fmt.Errorf("decoding info: %w", err)
	}
	torrent.Info = *info
	torrent.InfoHash = d.infoHash

	return torrent, nil
}

func (d *Decoder) getInfo(info map[string]any) (*Info, error) {
	result := &Info{}

	nameBytes, ok := info[NAME].([]byte)
	if !ok || len(nameBytes) == 0 {
		return nil, fmt.Errorf("missing or invalid info.name")
	}
	result.Name = filepath.Clean(strings.TrimLeft(string(nameBytes), "/"))

	if nameUTF8, ok := info[NAME_UTF8].([]byte); ok {
		s := filepath.Clean(strings.TrimLeft(string(nameUTF8), "/"))
		result.NameUTF8 = &s
	}

	pieceLength, ok := info[PIECE_LENGTH].(int64)
	if !ok || pieceLength <= 0 {
		return nil, fmt.Errorf("missing or invalid info.piece length")
	}
	result.PieceLength = pieceLength

	pieces, ok := info[PIECES].([]byte)
	if !ok || len(pieces) == 0 {
		return nil, fmt.Errorf("missing or invalid info.pieces")
	}
	if len(pieces)%20 != 0 {
		return nil, fmt.Errorf("invalid info.pieces")
	}
	result.Pieces = pieces

	if private, ok := info[PRIVATE].(int64); ok {
		if private != 0 && private != 1 {
			return nil, fmt.Errorf("invalid info.private")
		}

		privateBool := private == 1
		result.Private = &privateBool
	}

	if source, ok := info[SOURCE].([]byte); ok {
		s := string(source)
		result.Source = &s
	}

	if md5sum, ok := info[MD5SUM].([]byte); ok {
		s := string(md5sum)
		result.Md5Sum = &s
	}

	var length *int64
	if l, ok := info[LENGTH].(int64); ok {
		length = &l
		result.Length = length
	}

	var filesRaw []any
	if f, ok := info[FILES].([]any); ok {
		filesRaw = f
	}

	if length == nil && filesRaw == nil {
		return nil, fmt.Errorf("info must contain 'length' or 'files'")
	}

	if length != nil && filesRaw != nil {
		return nil, fmt.Errorf("info must have either 'length' or 'files', not both")
	}

	files, err := d.getFiles(filesRaw)
	if err != nil {
		return nil, err
	}
	result.Files = &files

	return result, nil
}

func (d *Decoder) getFiles(filesRaw []any) ([]File, error) {
	files := make([]File, 0, len(filesRaw))

	for i, fileRaw := range filesRaw {
		fileDict, ok := fileRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("files[%d]: invalid file entry", i)
		}

		length, ok := fileDict[LENGTH].(int64)
		if !ok {
			return nil, fmt.Errorf("files[%d]: missing or invalid length", i)
		}

		pathRaw, ok := fileDict[PATH].([]any)
		if !ok || len(pathRaw) == 0 {
			return nil, fmt.Errorf("files[%d]: missing or invalid path", i)
		}

		path := make([]string, 0, len(pathRaw))
		for _, segment := range pathRaw {
			segBytes, ok := segment.([]byte)
			if !ok {
				return nil, fmt.Errorf("files[%d]: invalid path segment", i)
			}
			path = append(path, filepath.Clean(string(segBytes)))
		}

		file := File{
			Length: length,
			Path:   path,
		}

		if pathUTF8Raw, ok := fileDict[PATH_UTF8].([]any); ok {
			pathUTF8 := make([]string, 0, len(pathUTF8Raw))
			for _, segment := range pathUTF8Raw {
				if segBytes, ok := segment.([]byte); ok {
					pathUTF8 = append(pathUTF8, string(segBytes))
				}
			}
			if len(pathUTF8) > 0 {
				file.PathUTF8 = &pathUTF8
			}
		}

		if md5sum, ok := fileDict[MD5SUM].([]byte); ok {
			s := string(md5sum)
			file.Md5Sum = &s
		}

		if attr, ok := fileDict[ATTR].([]byte); ok {
			s := string(attr)
			file.Attr = &s
		}

		files = append(files, file)
	}

	return files, nil
}
