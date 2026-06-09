package store_test

import (
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestRenameChildren(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()

	// Create directory tree: /photos/cat.jpg, /photos/sub/nested.txt
	files := []struct {
		id    string
		path  string
		isDir bool
	}{
		{"f-photos", "/photos", true},
		{"f-cat", "/photos/cat.jpg", false},
		{"f-sub", "/photos/sub", true},
		{"f-nested", "/photos/sub/nested.txt", false},
		{"f-other", "/other.txt", false},
	}
	for _, f := range files {
		if err := st.UpsertFile(&types.File{
			FileID: f.id, Name: f.path, Path: f.path, IsDir: f.isDir,
			CreatedAt: now, ModifiedAt: now, ModifiedBy: "nodeA",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Rename /photos → /pictures
	if err := st.RenameFile("f-photos", "/photos", "/pictures", "nodeA", now); err != nil {
		t.Fatal(err)
	}
	if err := st.RenameChildren("/photos", "/pictures", "nodeA", now); err != nil {
		t.Fatal(err)
	}

	// Verify parent renamed
	f, err := st.GetFileByID("f-photos")
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "/pictures" {
		t.Errorf("expected /pictures, got %s", f.Path)
	}

	// Verify children renamed
	for _, tc := range []struct {
		id       string
		wantPath string
	}{
		{"f-cat", "/pictures/cat.jpg"},
		{"f-sub", "/pictures/sub"},
		{"f-nested", "/pictures/sub/nested.txt"},
	} {
		f, err := st.GetFileByID(tc.id)
		if err != nil {
			t.Fatalf("GetFileByID(%s): %v", tc.id, err)
		}
		if f.Path != tc.wantPath {
			t.Errorf("%s: expected %s, got %s", tc.id, tc.wantPath, f.Path)
		}
	}

	// Verify unrelated file unchanged
	f, err = st.GetFileByID("f-other")
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "/other.txt" {
		t.Errorf("expected /other.txt, got %s", f.Path)
	}
}

func TestRenameChildrenEmptyDir(t *testing.T) {
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

	if err := st.RenameFile("f-empty", "/empty", "/renamed", "nodeA", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Should not error on empty directory
	if err := st.RenameChildren("/empty", "/renamed", "nodeA", time.Now()); err != nil {
		t.Fatal(err)
	}

	f, err := st.GetFileByID("f-empty")
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "/renamed" {
		t.Errorf("expected /renamed, got %s", f.Path)
	}
}
