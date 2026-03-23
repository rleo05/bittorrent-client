package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/rleo05/bittorrent-client/internal/bencode"
)

func main() {
	args := os.Args
	if len(args) < 2 {
		log.Fatal("missing .torrent file location")
	}

	fileLocation := args[1]

	if filepath.Ext(fileLocation) != ".torrent" {
		log.Fatal("file must have a .torrent extension")
	}

	file, err := os.Open(fileLocation)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer file.Close()

	const maxFileSize = 10 * 1024 * 1024
	limitReader := io.LimitReader(file, maxFileSize)

	torrentData, err := io.ReadAll(limitReader)
	if err != nil {
		log.Fatalf("error reading file: %v", err)
	}

	if len(torrentData) == maxFileSize {
		log.Fatal(".torrent file exceeds 10MB")
	}

	result, infoHash, err := bencode.Parse(torrentData)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result, infoHash)
}
