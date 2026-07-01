package chunk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveDeletesEmptyShardDirectory(t *testing.T) {
	dataDir := t.TempDir()
	s := New(dataDir)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	hash, _, err := s.Store(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}

	shardDir := filepath.Dir(s.Path(hash))
	if _, err := os.Stat(shardDir); err != nil {
		t.Fatalf("expected shard directory to exist: %v", err)
	}

	if err := s.Remove(hash); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(s.Path(hash)); !os.IsNotExist(err) {
		t.Fatalf("expected chunk file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(shardDir); !os.IsNotExist(err) {
		t.Fatalf("expected empty shard directory to be removed, got err=%v", err)
	}
}

func TestRemoveKeepsNonEmptyShardDirectory(t *testing.T) {
	dataDir := t.TempDir()
	s := New(dataDir)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	hash, _, err := s.Store(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}

	shardDir := filepath.Dir(s.Path(hash))
	otherFile := filepath.Join(shardDir, "extra")
	if err := os.WriteFile(otherFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Remove(hash); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(otherFile); err != nil {
		t.Fatalf("expected extra file to remain: %v", err)
	}
	if _, err := os.Stat(shardDir); err != nil {
		t.Fatalf("expected non-empty shard directory to remain: %v", err)
	}
}
