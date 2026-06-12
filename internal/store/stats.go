package store

// Storage stats

func (s *Store) ChunkCount() (int, error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM chunks`)
	var count int
	err := row.Scan(&count)
	return count, err
}

func (s *Store) TotalChunkBytes() (int64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM chunks`)
	var total int64
	err := row.Scan(&total)
	return total, err
}
