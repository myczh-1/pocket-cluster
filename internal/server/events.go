package server

import (
	"encoding/json"
	"fmt"
	"time"
	"github.com/google/uuid"
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

func (s *Server) PublishNodeUpdate(n *types.Node) error {
	_, err := s.appendEvent(types.EventNodeUpdate, n)
	return err
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
	case types.EventFileDelete:
		var payload struct {
			Path      string `json:"path"`
			DeletedBy string `json:"deleted_by"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		// Only tombstone the file. Physical chunk cleanup is deferred
		// to CleanupTombstones to avoid deleting data before peers sync.
		return s.store.MarkFileDeleted(payload.Path, payload.DeletedBy)
	case types.EventChunkReplicaAdd:
		var r types.Replica
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return err
		}
		return s.store.UpsertReplica(&r)
	case types.EventFileMove, types.EventFileRename:
		var payload struct {
			FileID          string `json:"file_id"`
			OldPath         string `json:"old_path"`
			NewPath         string `json:"new_path"`
			VersionID       string `json:"version_id"`
			ParentVersionID string `json:"parent_version_id"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		return s.store.RenameFile(payload.FileID, payload.OldPath, payload.NewPath, e.NodeID, e.Timestamp)
	case types.EventFileConflict:
		var payload struct {
			OriginalFileID    string `json:"original_file_id"`
			ConflictFileID    string `json:"conflict_file_id"`
			ConflictPath      string `json:"conflict_path"`
			OriginalVersionID string `json:"original_version_id"`
			ConflictVersionID string `json:"conflict_version_id"`
			ParentVersionID   string `json:"parent_version_id"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		return s.store.MarkFileConflict(payload.OriginalFileID, payload.ConflictFileID, payload.ConflictPath, payload.ConflictVersionID, payload.ParentVersionID, e.NodeID, e.Timestamp)
	case types.EventChunkReplicaRemove:
		var payload struct {
			ChunkID string `json:"chunk_id"`
			NodeID  string `json:"node_id"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		return s.store.MarkReplicaRemoved(payload.ChunkID, payload.NodeID, e.Timestamp)
	case types.EventNodeJoin, types.EventNodeUpdate:
		var n types.Node
		if err := json.Unmarshal(e.Payload, &n); err != nil {
			return err
		}
		s.sanitizeNodeAddress(&n)
		return s.store.UpdateNodeFull(&n)
	case types.EventDirCreate:
		var payload struct {
			Path      string `json:"path"`
			CreatedBy string `json:"created_by"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		return s.store.UpsertFile(&types.File{
			FileID:     uuid.New().String(),
			Name:       payload.Path,
			Path:       payload.Path,
			IsDir:      true,
			CreatedAt:  e.Timestamp,
			ModifiedAt: e.Timestamp,
			ModifiedBy: payload.CreatedBy,
		})
	case types.EventDirDelete:
		var payload struct {
			Path      string `json:"path"`
			DeletedBy string `json:"deleted_by"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return err
		}
		return s.store.MarkFileDeleted(payload.Path, payload.DeletedBy)
	case types.EventSnapshotCreated:
		return nil
	default:
		return fmt.Errorf("unsupported event type %s", e.Type)
	}
}

func (s *Server) sanitizeNodeAddress(n *types.Node) {
	n.AddressCandidates = filterLoopbackAddresses(n.AddressCandidates)
	if isLoopbackAddress(n.Address) {
		existing, err := s.store.GetNode(n.NodeID)
		if err == nil && existing.Address != "" && !isLoopbackAddress(existing.Address) {
			n.Address = existing.Address
		} else if len(n.AddressCandidates) > 0 {
			n.Address = n.AddressCandidates[0]
		}
	}
	if isLoopbackAddress(n.LastWorkingAddress) {
		n.LastWorkingAddress = ""
	}
}
