package store

import (
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestRecordFileVersionIsImmutableAndIdempotent(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	version := &types.File{
		FileID: "file", Name: "a.txt", Path: "/a.txt", VersionID: "version-1",
		ChunkIDs: []string{"chunk-a"}, CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000), ModifiedBy: "node-a",
	}
	if err := st.RecordFileVersion(version); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordFileVersion(version); err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	changed := *version
	changed.ChunkIDs = []string{"chunk-b"}
	if err := st.RecordFileVersion(&changed); err == nil {
		t.Fatal("reused version ID with different content was accepted")
	}
}

func TestMigrationV7SeedsCurrentFileVersions(t *testing.T) {
	dataDir := t.TempDir()
	st, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	file := &types.File{
		FileID: "file", Name: "a.txt", Path: "/a.txt", VersionID: "version-1",
		ParentVersionID: "version-0", ChunkIDs: []string{"chunk-a"},
		CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(2000), ModifiedBy: "node-a",
	}
	if err := st.UpsertFile(file); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM file_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE schema_version SET version = 6`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	got, err := upgraded.GetFileVersion(file.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFileVersion(got, file) {
		t.Fatalf("seeded version = %+v, want %+v", got, file)
	}
}

func TestSnapshotCarriesFileVersionGraph(t *testing.T) {
	source, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	base := &types.File{FileID: "file", Name: "a.txt", Path: "/a.txt", VersionID: "base", CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000)}
	next := &types.File{FileID: "file", Name: "a.txt", Path: "/a.txt", VersionID: "next", ParentVersionID: "base", CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(2000)}
	if err := source.RecordFileVersion(base); err != nil {
		t.Fatal(err)
	}
	if err := source.RecordFileVersion(next); err != nil {
		t.Fatal(err)
	}
	if err := source.UpsertFile(next); err != nil {
		t.Fatal(err)
	}
	if err := source.RenameFile(next.FileID, next.Path, "/renamed.txt", "node-a", time.UnixMilli(3000)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Versions) != 2 {
		t.Fatalf("snapshot versions = %d, want 2", len(snapshot.Versions))
	}

	target, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.LoadSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	versions, err := target.ListFileVersions("file")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].VersionID != "base" || versions[1].VersionID != "next" {
		t.Fatalf("loaded versions = %+v", versions)
	}
	current, err := target.GetFile("/renamed.txt")
	if err != nil {
		t.Fatal(err)
	}
	if current.VersionID != next.VersionID {
		t.Fatalf("loaded renamed view = %+v", current)
	}
}
