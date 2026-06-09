package store_test

import (
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestMarkChildrenDeleted(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()

	// Create a directory and files under it
	for _, p := range []string{"/photos", "/photos/cat.jpg", "/photos/dog.jpg", "/photos/sub/nested.txt", "/other.txt"} {
		isDir := p == "/photos" || p == "/photos/sub"
		if err := st.UpsertFile(&types.File{
			FileID: "f-" + p, Name: p, Path: p, IsDir: isDir,
			CreatedAt: now, ModifiedAt: now, ModifiedBy: "nodeA",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Recursively delete /photos
	if err := st.MarkChildrenDeleted("/photos", "nodeA"); err != nil {
		t.Fatal(err)
	}

	// /photos and children should be deleted
	for _, p := range []string{"/photos", "/photos/cat.jpg", "/photos/dog.jpg", "/photos/sub/nested.txt"} {
		f, err := st.GetFileByID("f-" + p)
		if err != nil {
			t.Fatalf("GetFileByID(%s): %v", p, err)
		}
		if !f.Deleted {
			t.Errorf("expected %s to be deleted", p)
		}
	}

	// /other.txt should NOT be deleted
	f, err := st.GetFileByID("f-/other.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Deleted {
		t.Error("/other.txt should not be deleted")
	}
}

func TestMarkChildrenDeletedEmptyDir(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(&types.File{
		FileID: "f-empty", Name: "empty", Path: "/empty", IsDir: true,
		CreatedAt: time.Now(), ModifiedAt: time.Now(), ModifiedBy: "nodeA",
	}); err != nil {
		t.Fatal(err)
	}

	// Should not error on empty directory
	if err := st.MarkChildrenDeleted("/empty", "nodeA"); err != nil {
		t.Fatal(err)
	}

	f, err := st.GetFileByID("f-empty")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Deleted {
		t.Error("expected /empty to be deleted")
	}
}
