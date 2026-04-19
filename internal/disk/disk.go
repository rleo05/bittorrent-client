package disk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

type Config struct {
	WriteChan   chan shared.DiskWrite
	Name        string
	Length      *uint64
	PieceLength uint64
	Files       *[]shared.File
	OutputRoot  string
}

type fileLayout struct {
	Path   string
	Start  int64
	End    int64
	Length int64
}

type Manager struct {
	Config

	layouts []fileLayout
}

func NewManager(cfg Config) *Manager {
	return &Manager{Config: cfg}
}

func (m *Manager) Prepare() error {
	if len(*m.Files) > 0 {
		outputDir := filepath.Join(m.OutputRoot, m.Name)
		if err := createDir(m.OutputRoot); err != nil {
			return err
		}
		if err := createMultiFilesParentDir(outputDir); err != nil {
			return err
		}

		m.layouts = make([]fileLayout, len(*m.Files))

		var startOffset int64
		for i, v := range *m.Files {
			fileName := filepath.Join(v.Path...)
			nextOffset, err := m.createFile(i, outputDir, fileName, v.Length, startOffset)
			if err != nil {
				return err
			}
			startOffset = nextOffset
		}

		return nil
	}

	m.layouts = make([]fileLayout, 1)

	_, err := m.createFile(0, m.OutputRoot, m.Name, *m.Length, 0)
	if err != nil {
		return err
	}

	return nil
}

func (m *Manager) createFile(index int, outputDir string, name string, length uint64, startOffset int64) (int64, error) {
	if err := createDir(m.OutputRoot); err != nil {
		return 0, err
	}

	targetPath := filepath.Join(outputDir, name)
	rel, err := filepath.Rel(outputDir, targetPath)
	if err != nil {
		return 0, fmt.Errorf("resolve target path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return 0, fmt.Errorf("resolved path escapes target root: %q", name)
	}

	parentDir := filepath.Dir(targetPath)
	if err := createDir(parentDir); err != nil {
		return 0, err
	}

	length64, err := castLength(length)
	if err != nil {
		return 0, err
	}
	if startOffset > 0 && startOffset > ((1<<63)-1)-length64 {
		return 0, fmt.Errorf("file offset exceeds int64: start=%d length=%d", startOffset, length)
	}

	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if err := f.Truncate(length64); err != nil {
		return 0, err
	}

	m.layouts[index] = fileLayout{
		Path:   targetPath,
		Start:  startOffset,
		End:    startOffset + length64,
		Length: length64,
	}

	return startOffset + length64, nil
}

func createDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	return nil
}

func createMultiFilesParentDir(path string) error {
	if err := os.Mkdir(path, 0o755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("target output already exists: %s", path)
		}
		return err
	}

	return nil
}

func castLength(length uint64) (int64, error) {
	if length > uint64((1<<63)-1) {
		return 0, fmt.Errorf("file length exceeds int64: %d", length)
	}

	return int64(length), nil
}

func (m *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-m.WriteChan:
			if !ok {
				return
			}

			fmt.Println("writing piece")
		}
	}
}
