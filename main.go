package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/rleo05/bittorrent-client/internal/bencode"
	"github.com/rleo05/bittorrent-client/internal/torrent"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Kill, syscall.SIGTERM)
	var wg sync.WaitGroup
	defer cancel()

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

	mapResult, ok := result.(map[string]any)
	if !ok {
		log.Fatal("invalid .torrent file")
	}

	t, err := torrent.NewDecoder(mapResult, infoHash).DecodeTorrent()
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	session := torrent.NewSession(t, port)
	session.Start(ctx, &wg)
	wg.Wait()
}
