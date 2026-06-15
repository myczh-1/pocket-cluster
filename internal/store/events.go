package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

// Event operations

func (s *Store) InsertEvent(e *types.Event) (bool, error) {
	res, err := insertEventExec(s.db, e)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

func (s *Store) GetEventsSince(sinceEventID string, limit int) ([]types.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows *sql.Rows
	var err error
	if sinceEventID == "" {
		rows, err = s.db.Query(`SELECT event_id, type, node_id, seq, timestamp, payload FROM events ORDER BY node_id ASC, seq ASC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT event_id, type, node_id, seq, timestamp, payload FROM events WHERE event_id > ? ORDER BY event_id ASC LIMIT ?`, sinceEventID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []types.Event
	for rows.Next() {
		var e types.Event
		var ts int64
		var typ string
		var payload string
		if err := rows.Scan(&e.EventID, &typ, &e.NodeID, &e.Seq, &ts, &payload); err != nil {
			return nil, err
		}
		e.Type = types.EventType(typ)
		e.Timestamp = time.UnixMilli(ts)
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) GetUnpushedEvents(peerNodeID string, limit int) ([]types.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT e.event_id, e.type, e.node_id, e.seq, e.timestamp, e.payload
		FROM events e
		WHERE NOT EXISTS (
			SELECT 1 FROM peer_pushed_events p
			WHERE p.peer_node_id = ? AND p.event_id = e.event_id
		)
		ORDER BY e.node_id ASC, e.seq ASC
		LIMIT ?`, peerNodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []types.Event
	for rows.Next() {
		var e types.Event
		var ts int64
		var typ string
		var payload string
		if err := rows.Scan(&e.EventID, &typ, &e.NodeID, &e.Seq, &ts, &payload); err != nil {
			return nil, err
		}
		e.Type = types.EventType(typ)
		e.Timestamp = time.UnixMilli(ts)
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) MarkEventsPushed(peerNodeID string, events []types.Event, pushedAt time.Time) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO peer_pushed_events (peer_node_id, event_id, pushed_at) VALUES (?, ?, ?)`,
			peerNodeID, e.EventID, timeMillis(pushedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) NextSeq(nodeID string) (int64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE node_id = ?`, nodeID)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

func (s *Store) UpsertFileWithEvent(f *types.File, nodeID string, eventType types.EventType, payload json.RawMessage, timestamp time.Time) (*types.Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	seq, err := nextSeqTx(tx, nodeID)
	if err != nil {
		return nil, err
	}
	e := &types.Event{
		EventID:   fmt.Sprintf("%s:%d", nodeID, seq),
		Type:      eventType,
		NodeID:    nodeID,
		Seq:       seq,
		Timestamp: timestamp,
		Payload:   payload,
	}
	if err := upsertFileTx(tx, f); err != nil {
		return nil, err
	}
	if _, err := insertEventExec(tx, e); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return e, nil
}

func nextSeqTx(tx *sql.Tx, nodeID string) (int64, error) {
	row := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE node_id = ?`, nodeID)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

func insertEventExec(exec sqlExecer, e *types.Event) (sql.Result, error) {
	return exec.Exec(`INSERT OR IGNORE INTO events (event_id, type, node_id, seq, timestamp, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		e.EventID, string(e.Type), e.NodeID, e.Seq, timeMillis(e.Timestamp), string(e.Payload))
}

func (s *Store) LatestEventID() (string, error) {
	row := s.db.QueryRow(`SELECT event_id FROM events ORDER BY timestamp DESC, node_id DESC, seq DESC LIMIT 1`)
	var eventID string
	if err := row.Scan(&eventID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return eventID, nil
}
