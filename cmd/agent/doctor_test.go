package main

import (
	"testing"
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
