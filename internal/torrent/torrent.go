package torrent

import "time"

type Torrent struct {
	Announce     *string
	AnnounceList [][]string

	CreationDate *time.Time
	Comment      *string
	CreatedBy    *string
	Encoding     *string

	HttpSeeds *[]string
	UrlList   *[]string
	Nodes     []Node

	Info     Info
	InfoHash [20]byte
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
	Files  *[]File
}

type File struct {
	Length int64

	Path     []string
	PathUTF8 *[]string

	Md5Sum *string
	Attr   *string
}

type Node struct {
	Host string
	Port int
}
