package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/pocketcluster/agent/internal/types"
)

// File operations

func (s *Store) UpsertFile(f *types.File) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertFileTx(tx, f); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetFile(path string) (*types.File, error) {
	row := s.db.QueryRow(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE path = ? AND deleted = 0`, path)
	return scanFile(row)
}

func (s *Store) GetFileByID(fileID string) (*types.File, error) {
	row := s.db.QueryRow(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE file_id = ?`, fileID)
	return scanFile(row)
}

func (s *Store) ListFiles(dirPath string) ([]types.File, error) {
	var rows *sql.Rows
	var err error
	if dirPath == "/" || dirPath == "" {
		rows, err = s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0 AND path LIKE '/%' ESCAPE '\' AND INSTR(SUBSTR(path, 2), '/') = 0 ORDER BY path ASC`)
	} else {
		prefix := strings.TrimRight(dirPath, "/") + "/"
		escaped := escapeLike(prefix)
		rows, err = s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0 AND path LIKE ? ESCAPE '\' AND INSTR(SUBSTR(path, ?), '/') = 0 ORDER BY path ASC`, escaped+"%", len(prefix)+1)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}
// ListDescendants returns all non-deleted files under dirPath (recursively).
func (s *Store) ListDescendants(dirPath string) ([]types.File, error) {
	prefix := strings.TrimRight(dirPath, "/") + "/"
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0 AND (path = ? OR path LIKE ? || '%') ORDER BY path ASC`, dirPath, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}


// escapeLike escapes % and _ for SQLite LIKE patterns.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (s *Store) MarkFileDeleted(path string, deletedBy string) error {
	_, err := s.db.Exec(`UPDATE files SET deleted = 1, modified_by = ?, modified_at = ? WHERE path = ? AND deleted = 0`,
		deletedBy, time.Now().UnixMilli(), path)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM files_fts WHERE file_id IN (SELECT file_id FROM files WHERE path = ?)`, path)
	return err
}

// MarkChildrenDeleted soft-deletes all files and directories under dirPath (inclusive).
func (s *Store) MarkChildrenDeleted(dirPath string, deletedBy string) error {
	now := time.Now().UnixMilli()
	prefix := strings.TrimSuffix(dirPath, "/") + "/"
	_, err := s.db.Exec(`UPDATE files SET deleted = 1, modified_by = ?, modified_at = ? WHERE (path = ? OR path LIKE ? || '%') AND deleted = 0`,
		deletedBy, now, dirPath, prefix)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM files_fts WHERE file_id IN (SELECT file_id FROM files WHERE deleted = 1 AND (path = ? OR path LIKE ? || '%'))`, dirPath, prefix)
	return err
}

func (s *Store) PurgeFile(fileID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM file_chunks WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE file_id = ? AND deleted = 1`, fileID); err != nil {
		return err
	}
	if err := deleteFileIndexTx(tx, fileID); err != nil {
		return err
	}
	return tx.Commit()
}

// RenameChildren updates the paths of all children when a directory is renamed.
func (s *Store) RenameChildren(oldDirPath, newDirPath, modifiedBy string, modifiedAt time.Time) error {
	oldPrefix := strings.TrimSuffix(oldDirPath, "/") + "/"
	newPrefix := strings.TrimSuffix(newDirPath, "/") + "/"
	millis := timeMillis(modifiedAt)
	rows, err := s.db.Query(`SELECT file_id, path FROM files WHERE path LIKE ? || '%' AND deleted = 0`, oldPrefix)
	if err != nil {
		return err
	}
	type update struct {
		fileID string
		newP   string
		newN   string
	}
	var updates []update
	for rows.Next() {
		var fileID, childPath string
		if err := rows.Scan(&fileID, &childPath); err != nil {
			continue
		}
		rest := childPath[len(oldPrefix):]
		np := newPrefix + rest
		updates = append(updates, update{fileID: fileID, newP: np, newN: filepath.Base(np)})
	}
	rows.Close()
	for _, u := range updates {
		if _, err := s.db.Exec(`UPDATE files SET path = ?, name = ?, modified_by = ?, modified_at = ? WHERE file_id = ?`,
			u.newP, u.newN, modifiedBy, millis, u.fileID); err != nil {
			return err
		}
		// Update FTS
		s.db.Exec(`DELETE FROM files_fts WHERE file_id = ?`, u.fileID)
		s.db.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, u.fileID, u.newN, u.newP)
	}
	return nil
}

func (s *Store) RenameFile(fileID, oldPath, newPath string, modifiedBy string, modifiedAt time.Time) error {
	name := filepath.Base(newPath)
	_, err := s.db.Exec(`UPDATE files SET path = ?, name = ?, modified_by = ?, modified_at = ? WHERE file_id = ? AND path = ? AND deleted = 0`,
		newPath, name, modifiedBy, timeMillis(modifiedAt), fileID, oldPath)
	if err != nil {
		return err
	}
	if err := s.deleteFileIndex(fileID); err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, fileID, name, newPath)
	return err
}

func (s *Store) MarkFileConflict(originalFileID, conflictFileID, conflictPath, conflictVersionID, parentVersionID, modifiedBy string, modifiedAt time.Time) error {
	name := filepath.Base(conflictPath)
	ts := timeMillis(modifiedAt)
	res, err := s.db.Exec(`UPDATE files SET path = ?, name = ?, version_id = ?, parent_version_id = ?, conflict_of = ?, modified_by = ?, modified_at = ? WHERE file_id = ?`,
		conflictPath, name, conflictVersionID, parentVersionID, originalFileID, modifiedBy, ts, conflictFileID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 0 {
		return s.indexFile(&types.File{FileID: conflictFileID, Name: name, Path: conflictPath})
	}
	_, err = s.db.Exec(`INSERT INTO files (file_id, name, path, version_id, parent_version_id, created_at, modified_at, modified_by, conflict_of) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		conflictFileID, name, conflictPath, conflictVersionID, parentVersionID, ts, ts, modifiedBy, originalFileID)
	if err != nil {
		return err
	}
	return s.indexFile(&types.File{FileID: conflictFileID, Name: name, Path: conflictPath})
}

func (s *Store) ListAllFilesIncludingDeleted() ([]types.File, error) {
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files ORDER BY path ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

func (s *Store) ListAllFiles() ([]types.File, error) {
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

func (s *Store) SearchFiles(keyword string) ([]types.File, error) {
	query := fileSearchQuery(keyword)
	if query == "" {
		return []types.File{}, nil
	}
	rows, err := s.db.Query(`SELECT f.file_id, f.name, f.path, f.is_dir, f.size_bytes, f.mime_type, f.version_id, f.parent_version_id, f.chunk_ids, f.created_at, f.modified_at, f.modified_by, f.deleted, f.conflict_of
		FROM files f
		JOIN files_fts ON files_fts.file_id = f.file_id
		WHERE f.deleted = 0 AND files_fts MATCH ?
		ORDER BY f.path ASC`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

func (s *Store) indexFile(f *types.File) error {
	return indexFileTx(s.db, f)
}

func upsertFileTx(tx *sql.Tx, f *types.File) error {
	chunkJSON, err := json.Marshal(f.ChunkIDs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO files
		(file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.FileID, f.Name, f.Path, boolToInt(f.IsDir), f.SizeBytes, f.MimeType,
		f.VersionID, f.ParentVersionID, string(chunkJSON),
		f.CreatedAt.UnixMilli(), f.ModifiedAt.UnixMilli(), f.ModifiedBy,
		boolToInt(f.Deleted), f.ConflictOf); err != nil {
		return err
	}
	if err := indexFileTx(tx, f); err != nil {
		return err
	}
	return upsertFileChunksTx(tx, f.FileID, f.ChunkIDs)
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func indexFileTx(exec sqlExecer, f *types.File) error {
	if err := deleteFileIndexTx(exec, f.FileID); err != nil {
		return err
	}
	if f.Deleted {
		return nil
	}
	_, err := exec.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, f.FileID, f.Name, f.Path)
	return err
}

func upsertFileChunksTx(exec sqlExecer, fileID string, chunkIDs []string) error {
	if _, err := exec.Exec(`DELETE FROM file_chunks WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for i, chunkID := range chunkIDs {
		if _, err := exec.Exec(`INSERT INTO file_chunks (file_id, chunk_id, position) VALUES (?, ?, ?)`, fileID, chunkID, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deleteFileIndex(fileID string) error {
	return deleteFileIndexTx(s.db, fileID)
}

func deleteFileIndexTx(exec sqlExecer, fileID string) error {
	_, err := exec.Exec(`DELETE FROM files_fts WHERE file_id = ?`, fileID)
	return err
}

func fileSearchQuery(keyword string) string {
	var parts []string
	var token []rune
	flush := func() {
		if len(token) == 0 {
			return
		}
		parts = append(parts, `"`+string(token)+`"*`)
		token = token[:0]
	}
	for _, r := range keyword {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			token = append(token, unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return strings.Join(parts, " ")
}

func isDirectChild(parent, child string) bool {
	if parent == "" {
		parent = "/"
	}
	if parent == "/" {
		if child == "/" || !strings.HasPrefix(child, "/") {
			return false
		}
		return !strings.Contains(child[1:], "/")
	}
	prefix := strings.TrimRight(parent, "/") + "/"
	if !strings.HasPrefix(child, prefix) {
		return false
	}
	return !strings.Contains(child[len(prefix):], "/")
}
