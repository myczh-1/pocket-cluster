package types

import (
	"encoding/json"
	"time"
)

type Node struct {
	NodeID         string    `json:"node_id"`
	Name           string    `json:"name"`
	Platform       string    `json:"platform"`
	Address        string    `json:"address"`
	PublicKey      string    `json:"public_key"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	Status         string    `json:"status"`
	Trusted        bool      `json:"trusted"`
	LastSeen       time.Time `json:"last_seen"`
	JoinedAt       time.Time `json:"joined_at"`
}

type File struct {
	FileID          string    `json:"file_id"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	IsDir           bool      `json:"is_dir"`
	SizeBytes       int64     `json:"size_bytes"`
	MimeType        string    `json:"mime_type,omitempty"`
	VersionID       string    `json:"version_id"`
	ParentVersionID string    `json:"parent_version_id"`
	ChunkIDs        []string  `json:"chunk_ids,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	ModifiedAt      time.Time `json:"modified_at"`
	ModifiedBy      string    `json:"modified_by"`
	Deleted         bool      `json:"deleted"`
	ConflictOf      string    `json:"conflict_of,omitempty"`
}

type Chunk struct {
	ChunkID   string    `json:"chunk_id"`
	SizeBytes int64     `json:"size_bytes"`
	StoredAt  time.Time `json:"stored_at"`
}

type Replica struct {
	ChunkID    string    `json:"chunk_id"`
	NodeID     string    `json:"node_id"`
	Status     string    `json:"status"`
	StoredAt   time.Time `json:"stored_at"`
	VerifiedAt time.Time `json:"verified_at"`
}

type EventType string

const (
	EventNodeJoin           EventType = "NODE_JOIN"
	EventNodeUpdate         EventType = "NODE_UPDATE"
	EventFilePut            EventType = "FILE_PUT"
	EventFileDelete         EventType = "FILE_DELETE"
	EventFileRename         EventType = "FILE_RENAME"
	EventFileConflict       EventType = "FILE_CONFLICT"
	EventChunkReplicaAdd    EventType = "CHUNK_REPLICA_ADD"
	EventChunkReplicaRemove EventType = "CHUNK_REPLICA_REMOVE"
	EventSnapshotCreated    EventType = "SNAPSHOT_CREATED"
)

type Event struct {
	EventID   string          `json:"event_id"`
	Type      EventType       `json:"type"`
	NodeID    string          `json:"node_id"`
	Seq       int64           `json:"seq"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}
type Cluster struct {
	ClusterID string    `json:"cluster_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Version   int64     `json:"version"`
}

type ReplicaStatus string

const (
	ReplicaHealthy         ReplicaStatus = "healthy"
	ReplicaUnderReplicated ReplicaStatus = "under_replicated"
	ReplicaUnavailable     ReplicaStatus = "unavailable"
)

type Invite struct {
	TokenHash string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	UsedAt    time.Time `json:"used_at,omitempty"`
	CreatedBy string    `json:"created_by"`
}

type JoinRequest struct {
	JoinToken  string     `json:"join_token"`
	NodeID     string     `json:"node_id"`
	PublicKey  string     `json:"public_key"`
	DeviceInfo DeviceInfo `json:"device_info"`
}

type DeviceInfo struct {
	Name           string `json:"name"`
	Platform       string `json:"platform"`
	Address        string `json:"address,omitempty"`
	TotalBytes     int64  `json:"total_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
}

type JoinResponse struct {
	ClusterID     string    `json:"cluster_id"`
	Approved      bool      `json:"approved"`
	ExistingNodes []NodeRef `json:"existing_nodes"`
}

type NodeRef struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name,omitempty"`
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
}

type APIResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *APIError       `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
