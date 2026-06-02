package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) appendEvent(eventType types.EventType, payload any) (*types.Event, error) {
	seq, err := s.store.NextSeq(s.cfg.NodeID)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	e := &types.Event{
		EventID:   fmt.Sprintf("%s:%d", s.cfg.NodeID, seq),
		Type:      eventType,
		NodeID:    s.cfg.NodeID,
		Seq:       seq,
		Timestamp: time.Now(),
		Payload:   body,
	}
	if _, err := s.store.InsertEvent(e); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Server) applyEvent(e types.Event) error {
	switch e.Type {
	case types.EventFilePut:
		var f types.File
		if err := json.Unmarshal(e.Payload, &f); err != nil {
			return err
		}
		if err := s.prepareFilePut(&f); err != nil {
			return err
		}
		return s.store.UpsertFile(&f)
	case types.EventChunkReplicaAdd:
		var r types.Replica
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return err
		}
		return s.store.UpsertReplica(&r)
	case types.EventNodeJoin, types.EventNodeUpdate:
		var n types.Node
		if err := json.Unmarshal(e.Payload, &n); err != nil {
			return err
		}
		return s.store.UpsertNode(&n)
	default:
		return nil
	}
}
