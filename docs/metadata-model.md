# Metadata Model

## Overview

Every node stores the complete metadata set. Metadata is eventually consistent across all nodes through Event Log + Snapshot synchronization.

## Entities

### Node

```json
{
  "node_id": "nodeA",
  "name": "MacBook Pro",
  "platform": "darwin",
  "address": "192.168.1.10:7788",
  "public_key": "base64-ed25519-pubkey",
  "total_bytes": 500000000000,
  "used_bytes": 120000000000,
  "available_bytes": 380000000000,
  "status": "online",
  "trusted": true,
  "last_seen": 1710000000000,
  "joined_at": 1709000000000
}
```

Fields:

- `node_id`: stable, generated on first start, persisted.
- `name`: human-readable device name.
- `platform`: `windows`, `darwin`, `linux`, `android`.
- `address`: current reachable `ip:port` on the local network.
- `public_key`: Ed25519 public key for signature verification.
- `total_bytes`: total disk space the node contributes.
- `used_bytes`: total bytes used by PocketCluster on this node.
- `available_bytes`: space available for PocketCluster use.
- `status`: `online` or `offline`.
- `trusted`: whether this node is in the trusted set.
- `last_seen`: unix millis of last successful communication.
- `joined_at`: unix millis when the node joined the cluster.

### Cluster

```json
{
  "cluster_id": "pocket-abc123",
  "name": "Home Pool",
  "created_at": 1709000000000,
  "version": 1
}
```

### File

```json
{
  "file_id": "f-001",
  "name": "photo.jpg",
  "path": "/photo.jpg",
  "is_dir": false,
  "size_bytes": 5242880,
  "mime_type": "image/jpeg",
  "version_id": "v-abc123",
  "parent_version_id": "",
  "chunk_ids": ["sha256-aaa"],
  "created_at": 1710000000000,
  "modified_at": 1710000000000,
  "modified_by": "nodeA",
  "deleted": false,
  "conflict_of": ""
}
```

Fields:

- `file_id`: UUID, assigned at creation, stable across renames.
- `name`: filename within parent directory.
- `path`: full path from root `/`.
- `is_dir`: true for directories.
- `size_bytes`: total file size (0 for directories).
- `mime_type`: detected MIME type (empty for directories).
- `version_id`: `sha256(file_id + parent_version_id + chunk_ids + node_id + timestamp)`.
- `parent_version_id`: version_id this update was based on. Empty for first version.
- `chunk_ids`: ordered list of chunk hashes that make up this file.
- `created_at`: unix millis.
- `modified_at`: unix millis of latest version.
- `modified_by`: node_id that produced the latest version.
- `deleted`: soft-delete flag. Deleted files remain in metadata until event log cleanup.
- `conflict_of`: if this is a conflict file, points to the original file_id.

### Chunk

```json
{
  "chunk_id": "sha256-aaa",
  "size_bytes": 67108864,
  "stored_at": 1710000000000
}
```

Fields:

- `chunk_id`: `sha256(chunk_content)`.
- `size_bytes`: chunk payload size in bytes.
- `stored_at`: unix millis when this node first stored the chunk.

### Replica

```json
{
  "chunk_id": "sha256-aaa",
  "node_id": "nodeA",
  "status": "available",
  "stored_at": 1710000000000,
  "verified_at": 1710000000000
}
```

Fields:

- `chunk_id`: references the chunk.
- `node_id`: which node holds this replica.
- `status`: `available`, `syncing`, or `missing`.
- `stored_at`: unix millis when replica was created on this node.
- `verified_at`: unix millis of last integrity check.

### Event

```json
{
  "event_id": "nodeA:101",
  "type": "FILE_PUT",
  "node_id": "nodeA",
  "seq": 101,
  "timestamp": 1710000000000,
  "payload": {}
}
```

Fields:

- `event_id`: `node_id:seq`.
- `type`: event type enum.
- `node_id`: originating node.
- `seq`: monotonically increasing sequence number on the originating node.
- `timestamp`: unix millis when the event was created.
- `payload`: event-type-specific data.

### Snapshot

```json
{
  "snapshot_id": "snap-001",
  "created_at": 1710000000000,
  "created_by": "nodeA",
  "last_event_id": "nodeA:1000",
  "cluster": {},
  "nodes": [],
  "files": [],
  "chunks": [],
  "replicas": []
}
```

Fields:

- `snapshot_id`: unique identifier.
- `created_at`: unix millis.
- `created_by`: node_id that created this snapshot.
- `last_event_id`: the last event included in this snapshot.
- `cluster`: cluster metadata at snapshot time.
- `nodes`: all node records.
- `files`: all file records (including deleted).
- `chunks`: all chunk records known at snapshot time.
- `replicas`: all replica records at snapshot time.

## Event Payload Schemas

### NODE_JOIN

```json
{
  "node_id": "nodeB",
  "name": "Pixel 7",
  "platform": "android",
  "public_key": "base64-ed25519-pubkey"
}
```

### NODE_UPDATE

```json
{
  "node_id": "nodeB",
  "total_bytes": 128000000000,
  "used_bytes": 30000000000,
  "available_bytes": 98000000000
}
```

### FILE_PUT

```json
{
  "file_id": "f-001",
  "name": "photo.jpg",
  "path": "/photo.jpg",
  "is_dir": false,
  "size_bytes": 5242880,
  "mime_type": "image/jpeg",
  "version_id": "v-abc123",
  "parent_version_id": "",
  "chunk_ids": ["sha256-aaa"]
}
```

### FILE_DELETE

```json
{
  "file_id": "f-001",
  "path": "/photo.jpg",
  "version_id": "v-del001",
  "parent_version_id": "v-abc123"
}
```

### FILE_RENAME

```json
{
  "file_id": "f-001",
  "old_path": "/photo.jpg",
  "new_path": "/photos/vacation.jpg",
  "version_id": "v-ren001",
  "parent_version_id": "v-abc123"
}
```

### FILE_CONFLICT

```json
{
  "original_file_id": "f-001",
  "conflict_file_id": "f-001-conflict",
  "conflict_path": "/photo.conflict.nodeB.1710000100.jpg",
  "original_version_id": "v-abc124",
  "conflict_version_id": "v-abc125",
  "parent_version_id": "v-abc123"
}
```

### CHUNK_REPLICA_ADD

```json
{
  "chunk_id": "sha256-aaa",
  "node_id": "nodeB"
}
```

### CHUNK_REPLICA_REMOVE

```json
{
  "chunk_id": "sha256-aaa",
  "node_id": "nodeB"
}
```

### SNAPSHOT_CREATED

```json
{
  "snapshot_id": "snap-001",
  "last_event_id": "nodeA:1000"
}
```

## Local Storage Layout

Each agent stores data in a local data directory.

```text
<data-dir>/
  config.json          # node config (node_id, keypair, cluster_id)
  metadata.db          # SQLite: nodes, files, chunks, replicas, events
  chunks/              # chunk payload storage
    <first-2-hex>/     # 2-char prefix directory for distribution
      <full-hash>      # raw chunk bytes
  snapshots/           # snapshot files
    <snapshot-id>.snap
```

## SQLite Tables

### nodes

| Column | Type | PK |
|--------|------|----|
| node_id | TEXT | yes |
| name | TEXT | |
| platform | TEXT | |
| address | TEXT | |
| public_key | TEXT | |
| total_bytes | INTEGER | |
| used_bytes | INTEGER | |
| available_bytes | INTEGER | |
| status | TEXT | |
| trusted | INTEGER | |
| last_seen | INTEGER | |
| joined_at | INTEGER | |

### files

| Column | Type | PK |
|--------|------|----|
| file_id | TEXT | yes |
| name | TEXT | |
| path | TEXT | unique |
| is_dir | INTEGER | |
| size_bytes | INTEGER | |
| mime_type | TEXT | |
| version_id | TEXT | |
| parent_version_id | TEXT | |
| chunk_ids | TEXT | (JSON array) |
| created_at | INTEGER | |
| modified_at | INTEGER | |
| modified_by | TEXT | |
| deleted | INTEGER | |
| conflict_of | TEXT | |

### chunks

| Column | Type | PK |
|--------|------|----|
| chunk_id | TEXT | yes |
| size_bytes | INTEGER | |
| stored_at | INTEGER | |

### replicas

| Column | Type | PK |
|--------|------|----|
| chunk_id | TEXT | (pk part) |
| node_id | TEXT | (pk part) |
| status | TEXT | |
| stored_at | INTEGER | |
| verified_at | INTEGER | |

### events

| Column | Type | PK |
|--------|------|----|
| event_id | TEXT | yes |
| type | TEXT | |
| node_id | TEXT | |
| seq | INTEGER | |
| timestamp | INTEGER | |
| payload | TEXT | (JSON) |
