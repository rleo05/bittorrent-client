package torrent

import (
	"time"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

type Torrent struct {
	Announce     *shared.Tracker
	AnnounceList [][]*shared.Tracker

	CreationDate *time.Time
	Comment      *string
	CreatedBy    *string
	Encoding     *string

	HttpSeeds *[]string
	UrlList   *[]string
	Nodes     []Node

	Info        Info
	InfoHash    [20]byte
	TotalLength uint64
}

type Info struct {
	Name     string
	NameUTF8 *string

	PieceLength uint64
	Pieces      []byte

	Private *bool
	Source  *string
	Md5Sum  *string

	Length *uint64
	Files  *[]shared.File
}

type Node struct {
	Host string
	Port int
}
