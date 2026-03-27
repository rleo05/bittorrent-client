package torrent

import (
	"net/url"
	"time"

	"github.com/rleo05/bittorrent-client/internal/types"
)

type Torrent struct {
	Announce     *url.URL
	AnnounceList [][]*url.URL

	CreationDate *time.Time
	Comment      *string
	CreatedBy    *string
	Encoding     *string

	HttpSeeds *[]string
	UrlList   *[]string
	Nodes     []Node

	Info        Info
	InfoHash    [20]byte
	TotalLength int64
}

type Info struct {
	Name     string
	NameUTF8 *string

	PieceLength int64
	Pieces      []byte

	Private *bool
	Source  *string
	Md5Sum  *string

	Length *int64
	Files  *[]types.File
}

type Node struct {
	Host string
	Port int
}
