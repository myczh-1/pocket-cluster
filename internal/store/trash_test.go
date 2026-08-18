package store

import (
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestTrashListsDeletedRootsAndRestoresDirectoryTree(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	files := []*types.File{
		{FileID: "dir", Name: "docs", Path: "/docs", IsDir: true},
		{FileID: "child", Name: "report.txt", Path: "/docs/report.txt", VersionID: "v1", ChunkIDs: []string{"chunk-a"}},
		{FileID: "single", Name: "single.txt", Path: "/single.txt", VersionID: "v2", ChunkIDs: []string{"chunk-b"}},
	}
	for _, file := range files {
		if err := st.UpsertFile(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkChildrenDeleted("/docs", "node-a"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFileDeleted("/single.txt", "node-a"); err != nil {
		t.Fatal(err)
	}
	trash, err := st.ListDeletedRoots()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 2 || trash[0].FileID == "child" || trash[1].FileID == "child" {
		t.Fatalf("trash roots = %+v, want directory root and standalone file", trash)
	}
	if err := st.RestoreDeleted("dir", true, "node-b", time.UnixMilli(5000)); err != nil {
		t.Fatal(err)
	}
	if err := st.RestoreDeleted("dir", true, "node-b", time.UnixMilli(5000)); err != nil {
		t.Fatalf("replayed restore failed: %v", err)
	}
	for _, path := range []string{"/docs", "/docs/report.txt"} {
		if _, err := st.GetFile(path); err != nil {
			t.Fatalf("restored %s: %v", path, err)
		}
	}
	results, err := st.SearchFiles("report")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].FileID != "child" {
		t.Fatalf("restored search results = %+v", results)
	}
	trash, err = st.ListDeletedRoots()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 || trash[0].FileID != "single" {
		t.Fatalf("trash after restore = %+v, want standalone file only", trash)
	}
}
