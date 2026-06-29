# API Contract

## Overview

All agents expose an HTTP REST + JSON API on the local network.

Default port: `7788`.

Base URL: `http://<node-ip>:7788`

All responses use `Content-Type: application/json` unless noted otherwise.

All requests between nodes must include a signature header for authentication.

## Current v0.1 Scope

This contract describes the current MVP snapshot that backs the existing Web UI and WebDAV flow.

### Supported

- Pool bootstrap, invite join, authentication, file browsing, upload, download, delete, and rename
- Peer-to-peer event sync, chunk transfer, snapshot bootstrap, and basic replica health visibility
- WebDAV access under `/dav/` using the same pool credentials as the Web UI

### Experimental / Rough Edges

- Android interoperability and background reliability remain environment-dependent
- Health endpoints expose replica summary and chunk-level detail, but do not yet cover a full jobs/task model
- Automatic repair exists, but explicit repair jobs and operator-triggered rescan/report APIs are not part of `v0.1`

### Explicitly Out Of Scope For v0.1

- Public Internet relay, NAT traversal, or cloud-hosted coordination
- Multi-user permissions, ACLs, or sharing APIs
- Dedicated jobs APIs such as `/api/jobs/*`

## Common Headers

### Node Authentication

```text
X-Node-ID: <node_id>
X-Signature: <base64-ed25519-signature>
X-Timestamp: <unix-millis>
X-Body-SHA256: <hex-sha256-of-request-body>
```

- Required for peer-to-peer metadata and chunk APIs: `/api/events`, `/api/events/push`, `/api/chunks`, and `/api/chunks/{hash}`.
- Signature message is newline-joined as: `method + "\n" + request_uri + "\n" + body_sha256 + "\n" + node_id + "\n" + timestamp`.
- `request_uri` includes the query string when present.
- Empty-body requests use SHA256 of the empty byte string.
- Receiving node verifies the signature against the sender's public key from trusted node metadata.
- Requests with expired timestamps (>5 min skew) are rejected.

### Response Envelope

Standard success:

```json
{ "ok": true, "data": { ... } }
```

Standard error:

```json
{ "ok": false, "error": { "code": "NOT_FOUND", "message": "..." } }
```

## Endpoints

### GET /api/node/info

Returns this node's own info.

**Response:**

```json
{
  "ok": true,
  "data": {
    "node_id": "nodeA",
    "name": "MacBook Pro",
    "platform": "darwin",
    "address": "192.168.1.10:7788",
    "total_bytes": 500000000000,
    "used_bytes": 120000000000,
    "available_bytes": 380000000000,
    "status": "online",
    "version": "0.1.0"
  }
}
```

### GET /api/nodes

Returns all known nodes.

**Response:**

```json
{
  "ok": true,
  "data": [
    {
      "node_id": "nodeA",
      "name": "MacBook Pro",
      "platform": "darwin",
      "address": "192.168.1.10:7788",
      "total_bytes": 500000000000,
      "used_bytes": 120000000000,
      "available_bytes": 380000000000,
      "status": "online",
      "last_seen": 1710000000000
    },
    {
      "node_id": "nodeB",
      "name": "Pixel 7",
      "platform": "android",
      "address": "192.168.1.20:7788",
      "total_bytes": 128000000000,
      "used_bytes": 30000000000,
      "available_bytes": 98000000000,
      "status": "offline",
      "last_seen": 1709999000000
    }
  ]
}
```

### POST /api/invites

Create a one-time invite token from an existing node UI.

**Response:**

```json
{
  "ok": true,
  "data": {
    "join_token": "random-token",
    "expires_at": "2026-06-02T10:15:00Z"
  }
}
```

Tokens are valid for 15 minutes and are consumed by the first successful join.

### POST /api/join/request

New node requests to join the cluster.

**Request:**

```json
{
  "join_token": "random-token",
  "node_id": "nodeC",
  "public_key": "base64-ed25519-pubkey",
  "device_info": {
    "name": "Old ThinkPad",
    "platform": "windows",
    "total_bytes": 256000000000,
    "available_bytes": 200000000000
  }
}
```

**Response (success):**

```json
{
  "ok": true,
  "data": {
    "cluster_id": "xxx",
    "approved": true,
    "existing_nodes": [
      { "node_id": "nodeA", "address": "192.168.1.10:7788", "public_key": "base64-ed25519-pubkey" },
      { "node_id": "nodeB", "address": "192.168.1.20:7788", "public_key": "base64-ed25519-pubkey" }
    ]
  }
}
```

**Response (token invalid/expired):**

```json
{
  "ok": false,
  "error": { "code": "JOIN_TOKEN_INVALID", "message": "Token expired or already used" }
}
```

### POST /api/join/approve

Used when join requires manual approval (future UI flow).

**Request:**

```json
{
  "node_id": "nodeC",
  "public_key": "base64-ed25519-pubkey",
  "device_info": { ... }
}
```

**Response:**

```json
{
  "ok": true,
  "data": {
    "approved": true,
    "existing_nodes": [ ... ]
  }
}
```

### GET /api/files

List files in the metadata tree.

**Query parameters:**

| Parameter | Type   | Default | Description                |
|-----------|--------|---------|----------------------------|
| `path`    | string | `/`     | Directory path to list     |
| `recursive` | bool | `false` | Include subdirectories recursively |

**Response:**

```json
{
  "ok": true,
  "data": {
    "path": "/",
    "entries": [
      {
        "file_id": "f-001",
        "name": "photo.jpg",
        "path": "/photo.jpg",
        "is_dir": false,
        "size_bytes": 5242880,
        "mime_type": "image/jpeg",
        "chunk_count": 1,
        "version_id": "v-abc123",
        "replica_status": "healthy",
        "created_at": 1710000000000,
        "modified_at": 1710000000000,
        "modified_by": "nodeA"
      },
      {
        "file_id": "f-002",
        "name": "Documents",
        "path": "/Documents",
        "is_dir": true,
        "entry_count": 5,
        "created_at": 1710000000000,
        "modified_at": 1710000000000
      }
    ]
  }
}
```

### POST /api/files/upload

Upload a file. Content-Type must be `multipart/form-data`.

**Form fields:**

| Field  | Type         | Description              |
|--------|-------------|--------------------------|
| `path` | string      | Target path in pool      |
| `file` | file stream | File content             |

**Processing:**

1. Receive file content.
2. Split into 64MB chunks.
3. Compute SHA256 for each chunk.
4. Store chunks locally.
5. Create file metadata with chunk list.
6. Append `FILE_PUT` event.
7. Trigger replication to other nodes.

**Response:**

```json
{
  "ok": true,
  "data": {
    "file_id": "f-003",
    "path": "/report.pdf",
    "size_bytes": 134217728,
    "chunk_count": 2,
    "version_id": "v-def456",
    "replica_status": "under_replicated"
  }
}
```

### GET /api/files/download?path=/report.pdf

Download a file by path.

**Query parameters:**

| Parameter | Type   | Required | Description      |
|-----------|--------|----------|------------------|
| `path`    | string | yes      | File path in pool|

**Response:** `Content-Type: application/octet-stream` with file content.

Chunks are reassembled in order and streamed to the client.

### GET /api/files/download?id=f-003

Download a file by file_id.

**Response:** Same as path-based download.
### DELETE /api/files?path=/report.pdf
Delete a file or directory. Directories are recursively deleted (all children are tombstoned).
**Query parameters:**
| Parameter | Type   | Required | Description            |
|-----------|--------|----------|------------------------|
| `path`    | string | yes      | File or directory path |
**Response:**
```json
{
  "ok": true,
  "data": {
    "path": "/report.pdf",
    "status": "deleted"
  }
}
```
Deleted files are tombstoned (soft-deleted) and retained for 7 days before permanent removal.
### PATCH /api/files/rename
Rename or move a file.
**Request body (JSON):**
| Field     | Type   | Required | Description     |
|-----------|--------|----------|-----------------|
| `path`    | string | yes      | Current path    |
| `new_path`| string | yes      | New path        |
**Response:**
```json
{
  "ok": true,
  "data": {
    "file_id": "f-001",
    "old_path": "/report.pdf",
    "new_path": "/Documents/report.pdf"
  }
}
```

### GET /api/chunks/{hash}

Return chunk metadata and content.

**Path parameter:** `hash` = SHA256 of the chunk.

**Response (HEAD):** `Content-Length: <chunk-size-bytes>`

**Response (GET):** `Content-Type: application/octet-stream` with raw chunk bytes.

### POST /api/chunks

Store a chunk from another node.

**Request:** `Content-Type: application/octet-stream`

**Headers:**

```text
X-Node-ID: <node_id>
X-Signature: <base64-ed25519-signature>
X-Timestamp: <unix-millis>
X-Body-SHA256: <sha256>
X-Chunk-Hash: <sha256>
Content-Length: <size>
```

**Response:**

```json
{
  "ok": true,
  "data": {
    "hash": "sha256...",
    "size_bytes": 67108864,
    "stored": true
  }
}
```

### GET /api/events

Fetch events from the event log.

**Query parameters:**

| Parameter | Type   | Required | Description                          |
|-----------|--------|----------|--------------------------------------|
| `since`   | string | no       | Event ID to start after (e.g. `nodeA:100`) |
| `limit`   | int    | no       | Max events to return (default 1000)  |

**Response:**

```json
{
  "ok": true,
  "data": {
    "events": [
      {
        "event_id": "nodeA:101",
        "type": "FILE_PUT",
        "timestamp": 1710000000000,
        "payload": {
          "file_id": "f-003",
          "path": "/report.pdf",
          "version_id": "v-def456",
          "parent_version_id": "",
          "chunk_ids": ["sha256-aaa", "sha256-bbb"],
          "size_bytes": 134217728
        }
      }
    ],
    "has_more": false
  }
}
```

### POST /api/events/push

Push events from another node.

**Request:**

```json
{
  "events": [
    {
      "event_id": "nodeB:50",
      "type": "FILE_PUT",
      "timestamp": 1710000010000,
      "payload": { ... }
    }
  ]
}
```

**Response:**

```json
{
  "ok": true,
  "data": {
    "accepted": 1,
    "conflicts": []
  }
}
```

### GET /api/snapshot

Fetch the latest metadata snapshot.

**Response:** `Content-Type: application/octet-stream` with snapshot data.

Snapshot contains full metadata state: nodes, files, chunks, replicas.

### GET /api/health

Simple health check. No authentication required.

**Response:**

```json
{
  "ok": true,
  "data": {
    "node_id": "nodeA",
    "status": "online",
    "uptime_seconds": 3600
  }
}
```

### GET /api/health/summary
Returns the overall replica health summary of the pool.
**Response:**
```json
{
  "ok": true,
  "data": {
    "total_files": 15,
    "total_chunks": 42,
    "healthy_chunks": 38,
    "under_replicated_chunks": 3,
    "unavailable_chunks": 1,
    "repairing_chunks": 0,
    "overall_status": "under_replicated",
    "last_scan_at": "2026-06-09T10:00:00Z",
    "last_repair_at": "2026-06-09T09:55:00Z"
  }
}
```
### GET /api/health/chunks
Returns per-chunk health details with replica node information.
**Response:**
```json
{
  "ok": true,
  "data": [
    {
      "chunk_id": "sha256...",
      "size_bytes": 67108864,
      "replica_count": 2,
      "online_replicas": 2,
      "target_replicas": 2,
      "status": "healthy",
      "replica_nodes": [
        { "node_id": "nodeA", "status": "available", "online": true, "has_chunk": true },
        { "node_id": "nodeB", "status": "available", "online": true, "has_chunk": true }
      ],
      "referencing_files": ["/photo.jpg"]
    }
  ]
}
```
### GET /api/health/chunks/{hash}
Returns health detail for a specific chunk.
**Path parameter:** `hash` = SHA256 of the chunk.
**Response:** Single chunk health detail object (same shape as items in `/api/health/chunks`).
### GET /api/health/files/{fileId}
Returns health detail for a specific file with per-chunk breakdown.
**Path parameter:** `fileId` = file ID.
**Response:**
```json
{
  "ok": true,
  "data": {
    "file_id": "f-001",
    "path": "/photo.jpg",
    "name": "photo.jpg",
    "size_bytes": 5242880,
    "chunk_count": 1,
    "status": "healthy",
    "chunks": [ ... ]
  }
}
```

### GET /api/sync/tasks

Returns the current in-memory sync task view used by the Web UI.

This endpoint is an early `v0.2` observability surface. It is intended for operator visibility and may evolve before a more formal jobs API is introduced.

**Response:**

```json
{
  "ok": true,
  "data": {
    "tasks": [
      {
        "id": "pull:nodeA",
        "kind": "metadata_pull",
        "status": "running",
        "title": "Pulling metadata",
        "target": "nodeA",
        "message": "Fetching remote events from this node.",
        "started_at": "2026-06-26T10:00:00Z",
        "updated_at": "2026-06-26T10:00:01Z"
      }
    ]
  }
}
```

### GET /api/jobs

Returns operator-triggered background jobs such as rescans and manual repair passes.

**Response:**

```json
{
  "ok": true,
  "data": {
    "jobs": [
      {
        "id": "job-123",
        "kind": "rescan",
        "status": "done",
        "title": "Rescanning health state",
        "message": "Health scan completed.",
        "created_at": "2026-06-27T10:00:00Z",
        "updated_at": "2026-06-27T10:00:01Z",
        "finished_at": "2026-06-27T10:00:01Z"
      }
    ]
  }
}
```

### GET /api/jobs/{jobId}

Returns a single operator-triggered background job by ID.

### POST /api/jobs/rescan

Starts a health rescan job.

**Response:** `202 Accepted` with the created job object.

### POST /api/jobs/repair-under-replicated

Starts a best-effort repair pass for currently under-replicated chunks.

**Response:** `202 Accepted` with the created job object.

### POST /api/jobs/integrity-check

Starts an integrity verification job that recomputes the SHA-256 hash of every locally stored chunk and compares it against the recorded chunk ID. Detects on-disk corruption and data loss. Chunks that pass verification have their `verified_at` timestamp refreshed.

**Response:** `202 Accepted` with the created job object. The job finishes with:
- `done` — all local chunks passed verification
- `retrying` — some chunks are missing from local disk (run repair to restore)
- `failed` — one or more chunks are corrupt (hash mismatch)
## Error Codes

| Code                | HTTP Status | Description                              |
|---------------------|-------------|------------------------------------------|
| `NOT_FOUND`         | 404         | Resource not found                       |
| `JOIN_TOKEN_INVALID`| 403         | Join token expired or already used       |
| `SIGNATURE_INVALID` | 401         | Node signature verification failed       |
| `CHUNK_NOT_FOUND`   | 404         | Requested chunk hash not stored locally  |
| `CONFLICT`          | 409         | File version conflict detected           |
| `INTERNAL_ERROR`    | 500         | Unexpected server error                  |
