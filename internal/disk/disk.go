package disk

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rleo05/bittorrent-client/internal/shared"
)

type Config struct {
	WriteChan       chan *shared.DiskWrite
	WriteResultChan chan *shared.DiskWriteResult
	CancelSession   context.CancelFunc
	Stats           *shared.Stats
	Name            string
	Length          *uint64
	PieceLength     uint64
	Files           *[]shared.File
	OutputRoot      string
	Completed       chan<- struct{}
	CompleteAck     <-chan struct{}
}

type fileLayout struct {
	Path   string
	Start  int64
	End    int64
	Length int64
	File   *os.File
}

type Manager struct {
	Config

	layouts []fileLayout
}

func NewManager(cfg Config) *Manager {
	return &Manager{Config: cfg}
}

func (m *Manager) Prepare() (err error) {
	defer func() {
		if err != nil {
			m.closeLayouts()
		}
	}()

	if err := createDir(m.OutputRoot); err != nil {
		return err
	}

	if m.Files != nil && len(*m.Files) > 0 {
		outputDir := filepath.Join(m.OutputRoot, m.Name)
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

	_, err = m.createFile(0, m.OutputRoot, m.Name, *m.Length, 0)
	if err != nil {
		return err
	}

	return nil
}

func (m *Manager) createFile(index int, outputDir string, name string, length uint64, startOffset int64) (int64, error) {
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

	if err := f.Truncate(length64); err != nil {
		f.Close()
		return 0, err
	}

	m.layouts[index] = fileLayout{
		Path:   targetPath,
		Start:  startOffset,
		End:    startOffset + length64,
		Length: length64,
		File:   f,
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
	defer m.closeLayouts()

	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-m.WriteChan:
			if !ok {
				return
			}

			fileIndex, err := m.findLayoutIndex(p.Offset)
			if err != nil {
				m.failWrite(ctx, p.PieceIndex, err)
				return
			}

			if err := m.handleWrite(fileIndex, p); err != nil {
				m.failWrite(ctx, p.PieceIndex, err)
				return
			}

			remaining, completed := m.recordWriteProgress(uint64(len(p.Data)))
			m.reportWriteResult(ctx, p.PieceIndex, nil)

			if completed {
				log.Printf("download completed: downloaded=%d left=%d", m.Stats.Downloaded.Load(), remaining)
				m.notifyCompletion(ctx)
				if m.CancelSession != nil {
					m.CancelSession()
				}
				return
			}
		}
	}
}

func (m *Manager) handleWrite(fileIndex int, p *shared.DiskWrite) error {
	data := p.Data
	offset := p.Offset

	for len(data) > 0 {
		if fileIndex < 0 || fileIndex >= len(m.layouts) {
			return fmt.Errorf("invalid file layout index: %d", fileIndex)
		}

		fileLayout := m.layouts[fileIndex]
		fileOffset := offset - fileLayout.Start
		if fileOffset < 0 || fileOffset >= fileLayout.Length {
			return fmt.Errorf("offset %d is outside file layout %q", offset, fileLayout.Path)
		}

		remainingInFile := fileLayout.Length - fileOffset
		bytesToWrite := min(int64(len(data)), remainingInFile)

		if fileLayout.File == nil {
			return fmt.Errorf("file layout %q has no open descriptor", fileLayout.Path)
		}

		if _, err := fileLayout.File.WriteAt(data[:bytesToWrite], fileOffset); err != nil {
			return err
		}

		data = data[bytesToWrite:]
		offset += bytesToWrite
		fileIndex++
	}

	return nil
}

func (m *Manager) findLayoutIndex(offset int64) (int, error) {
	for i, v := range m.layouts {
		if offset >= v.Start && offset < v.End {
			return i, nil
		}
	}

	return 0, fmt.Errorf("no file layout found for offset %d", offset)
}

func (m *Manager) closeLayouts() {
	for i := range m.layouts {
		if m.layouts[i].File == nil {
			continue
		}

		_ = m.layouts[i].File.Close()
		m.layouts[i].File = nil
	}
}

func (m *Manager) recordWriteProgress(written uint64) (uint64, bool) {
	if m.Stats == nil || written == 0 {
		return 0, false
	}

	m.Stats.Downloaded.Add(written)

	for {
		currentLeft := m.Stats.Left.Load()
		if written >= currentLeft {
			if m.Stats.Left.CompareAndSwap(currentLeft, 0) {
				return 0, true
			}
			continue
		}

		nextLeft := currentLeft - written
		if m.Stats.Left.CompareAndSwap(currentLeft, nextLeft) {
			return nextLeft, nextLeft == 0
		}
	}
}

func (m *Manager) notifyCompletion(ctx context.Context) {
	if m.Completed != nil {
		select {
		case m.Completed <- struct{}{}:
		case <-ctx.Done():
			return
		}
	}

	if m.CompleteAck == nil {
		return
	}

	select {
	case <-m.CompleteAck:
	case <-ctx.Done():
	}
}

func (m *Manager) failWrite(ctx context.Context, pieceIndex int, err error) {
	log.Printf("disk manager stopped: piece=%d error=%v", pieceIndex, err)
	m.reportWriteResult(ctx, pieceIndex, err)
	if m.CancelSession != nil {
		m.CancelSession()
	}
}

func (m *Manager) reportWriteResult(ctx context.Context, pieceIndex int, err error) {
	if m.WriteResultChan == nil {
		return
	}

	result := &shared.DiskWriteResult{
		PieceIndex: pieceIndex,
		Error:      err,
	}

	select {
	case m.WriteResultChan <- result:
	case <-ctx.Done():
	}
}
