package chunk

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const ChunkSize = 64 * 1024 * 1024 // 64MB

type Storage struct {
	chunkDir string
}

func New(dataDir string) *Storage {
	return &Storage{chunkDir: filepath.Join(dataDir, "chunks")}
}

func (s *Storage) Init() error {
	return os.MkdirAll(s.chunkDir, 0o755)
}

func (s *Storage) Store(r io.Reader) (hash string, size int64, err error) {
	h := sha256.New()
	f, err := os.CreateTemp(s.chunkDir, ".tmp-*")
	if err != nil {
		return "", 0, fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	size, err = io.Copy(f, io.TeeReader(r, h))
	if err != nil {
		return "", 0, fmt.Errorf("write chunk: %w", err)
	}
	if err = f.Close(); err != nil {
		return "", 0, fmt.Errorf("close tmp: %w", err)
	}

	hash = fmt.Sprintf("%x", h.Sum(nil))
	finalPath := s.Path(hash)
	if err = os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return "", 0, err
	}
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return "", 0, fmt.Errorf("rename chunk: %w", err)
	}
	// Verify integrity after write
	if err = s.Verify(hash); err != nil {
		os.Remove(finalPath)
		return "", 0, fmt.Errorf("verify chunk: %w", err)
	}
	return hash, size, nil
}

func (s *Storage) Open(hash string) (*os.File, int64, error) {
	p := s.Path(hash)
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, fmt.Errorf("open chunk: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (s *Storage) Exists(hash string) bool {
	_, err := os.Stat(s.Path(hash))
	return err == nil
}

func (s *Storage) Path(hash string) string {
	prefix := hash[:2]
	return filepath.Join(s.chunkDir, prefix, hash)
}

func (s *Storage) Remove(hash string) error {
	return os.Remove(s.Path(hash))
}

func (s *Storage) Verify(hash string) error {
	f, _, err := s.Open(hash)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != hash {
		return fmt.Errorf("chunk %s: hash mismatch, got %s", hash, actual)
	}
	return nil
}
