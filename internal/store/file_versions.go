package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

// RecordFileVersion stores an immutable file version. Replaying the same
// version is idempotent, while reusing a version ID for different metadata is
// rejected.
func (s *Store) RecordFileVersion(f *types.File) error {
	if f.VersionID == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordFileVersionTx(tx, f); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureFileVersion seeds lineage for a materialized file created before the
// version ledger existed. A renamed materialized view may legitimately have a
// different path from its immutable content-version record.
func (s *Store) EnsureFileVersion(f *types.File) error {
	if f.VersionID == "" {
		return nil
	}
	if _, err := s.GetFileVersion(f.VersionID); err == nil {
		return nil
	} else if err != sql.ErrNoRows {
		return err
	}
	return s.RecordFileVersion(f)
}

func recordFileVersionTx(tx *sql.Tx, f *types.File) error {
	if f.VersionID == "" {
		return nil
	}
	chunkJSON, err := json.Marshal(f.ChunkIDs)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO file_versions
		(version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.VersionID, f.FileID, f.ParentVersionID, f.Name, f.Path, boolToInt(f.IsDir), f.SizeBytes, f.MimeType,
		string(chunkJSON), timeMillis(f.CreatedAt), timeMillis(f.ModifiedAt), f.ModifiedBy, boolToInt(f.Deleted), f.ConflictOf)
	if err != nil {
		return err
	}
	inserted, err := res.RowsAffected()
	if err != nil || inserted != 0 {
		return err
	}
	existing, err := scanFileVersion(tx.QueryRow(`SELECT version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM file_versions WHERE version_id = ?`, f.VersionID))
	if err != nil {
		return err
	}
	if !sameFileVersion(existing, f) {
		return fmt.Errorf("file version %s was reused with different metadata", f.VersionID)
	}
	return nil
}

func (s *Store) GetFileVersion(versionID string) (*types.File, error) {
	return scanFileVersion(s.db.QueryRow(`SELECT version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM file_versions WHERE version_id = ?`, versionID))
}

func (s *Store) ListFileVersions(fileID string) ([]types.File, error) {
	rows, err := s.db.Query(`SELECT version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM file_versions WHERE file_id = ? ORDER BY version_id`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []types.File
	for rows.Next() {
		f, err := scanFileVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *f)
	}
	return versions, rows.Err()
}

func (s *Store) ListAllFileVersions() ([]types.File, error) {
	rows, err := s.db.Query(`SELECT version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM file_versions ORDER BY file_id, version_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []types.File
	for rows.Next() {
		f, err := scanFileVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *f)
	}
	return versions, rows.Err()
}

func listAllFileVersionsTx(tx *sql.Tx) ([]types.File, error) {
	rows, err := tx.Query(`SELECT version_id, file_id, parent_version_id, name, path, is_dir, size_bytes, mime_type, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM file_versions ORDER BY file_id, version_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []types.File
	for rows.Next() {
		f, err := scanFileVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *f)
	}
	return versions, rows.Err()
}

// ReplaceConcurrentFileViews atomically rebuilds the materialized main file
// and deterministic conflict copies for one logical file.
func (s *Store) ReplaceConcurrentFileViews(originalFileID string, views []*types.File) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT file_id, chunk_ids FROM files WHERE file_id = ? OR (conflict_of = ? AND instr(file_id, ?) = 1)`, originalFileID, originalFileID, originalFileID+".conflict.")
	if err != nil {
		return nil, err
	}
	var oldFileIDs []string
	var oldChunkIDs []string
	for rows.Next() {
		var fileID, chunkJSON string
		if err := rows.Scan(&fileID, &chunkJSON); err != nil {
			rows.Close()
			return nil, err
		}
		oldFileIDs = append(oldFileIDs, fileID)
		var chunks []string
		if err := json.Unmarshal([]byte(chunkJSON), &chunks); err != nil {
			rows.Close()
			return nil, err
		}
		oldChunkIDs = append(oldChunkIDs, chunks...)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, fileID := range oldFileIDs {
		if fileID == originalFileID {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM file_chunks WHERE file_id = ?`, fileID); err != nil {
			return nil, err
		}
		if err := deleteFileIndexTx(tx, fileID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM files WHERE file_id = ?`, fileID); err != nil {
			return nil, err
		}
	}
	for _, view := range views {
		if err := upsertFileTx(tx, view); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return oldChunkIDs, nil
}

func scanFileVersion(row scannable) (*types.File, error) {
	var f types.File
	var chunkJSON string
	var created, modified int64
	var isDir, deleted int
	if err := row.Scan(&f.VersionID, &f.FileID, &f.ParentVersionID, &f.Name, &f.Path, &isDir, &f.SizeBytes, &f.MimeType,
		&chunkJSON, &created, &modified, &f.ModifiedBy, &deleted, &f.ConflictOf); err != nil {
		return nil, err
	}
	f.IsDir = intToBool(isDir)
	f.Deleted = intToBool(deleted)
	f.CreatedAt = time.UnixMilli(created)
	f.ModifiedAt = time.UnixMilli(modified)
	if err := json.Unmarshal([]byte(chunkJSON), &f.ChunkIDs); err != nil {
		return nil, err
	}
	return &f, nil
}

func sameFileVersion(a, b *types.File) bool {
	return a.FileID == b.FileID && a.VersionID == b.VersionID && a.ParentVersionID == b.ParentVersionID &&
		a.Name == b.Name && a.Path == b.Path && a.IsDir == b.IsDir && a.SizeBytes == b.SizeBytes &&
		a.MimeType == b.MimeType && reflect.DeepEqual(a.ChunkIDs, b.ChunkIDs) &&
		a.CreatedAt.Equal(b.CreatedAt) && a.ModifiedAt.Equal(b.ModifiedAt) && a.ModifiedBy == b.ModifiedBy &&
		a.Deleted == b.Deleted && a.ConflictOf == b.ConflictOf
}
