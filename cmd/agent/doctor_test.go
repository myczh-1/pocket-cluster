package main

import (
	"testing"

	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
)

func TestCheckDataDir(t *testing.T) {
	// Test with non-existent directory
	result := checkDataDir("/tmp/nonexistent-pocketcluster-test")
	if result.Status != "warn" {
		t.Errorf("expected warn for non-existent dir, got %s", result.Status)
	}

	// Test with existing temp directory
	result = checkDataDir(t.TempDir())
	if result.Status != "warn" {
		t.Errorf("expected warn for empty dir, got %s", result.Status)
	}

	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	result = checkDataDir(dir)
	if result.Status != "ok" {
		t.Errorf("expected ok for dir with metadata.db, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckPort(t *testing.T) {
	// Port should be available
	result := checkPort(0) // Let OS pick a port
	if result.Status != "ok" {
		t.Errorf("expected ok for port check, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckStorageWritable(t *testing.T) {
	dir := t.TempDir()
	result := checkStorageWritable(dir)
	if result.Status != "ok" {
		t.Errorf("expected ok for writable dir, got %s: %s", result.Status, result.Message)
	}

	// Test read-only directory (may not work on all systems)
	result = checkStorageWritable("/dev/null/nonexistent")
	if result.Status != "fail" {
		t.Errorf("expected fail for unwritable path, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckConfigSummary(t *testing.T) {
	dir := t.TempDir()
	result := checkConfigSummary(dir)
	if result.Status != "warn" {
		t.Fatalf("expected warn for missing config, got %s", result.Status)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClusterID = "cluster-1"
	if err := cfg.SetPoolCredentials("admin", "testpass"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	result = checkConfigSummary(dir)
	if result.Status != "ok" {
		t.Fatalf("expected ok for configured cluster, got %s: %s", result.Status, result.Message)
	}
}
