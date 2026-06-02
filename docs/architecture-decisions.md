# Architecture Decisions

## Purpose

This document records PocketCluster decisions that are considered stable project foundations.

Changing any decision in this file should be treated as a major product and architecture change, not a casual implementation detail.

## Fixed Decisions

### 1. No Master Node

PocketCluster must not depend on a permanent Leader, Master, Coordinator, or central control node.

All nodes are peers.

Implications:

- Any node may be offline.
- The resource pool should continue operating with remaining online nodes.
- Metadata and storage design must not assume one node is always authoritative.

### 2. Full Metadata Sync

Each node stores the full metadata set:

- directory tree
- file information
- Chunk information
- Replica information
- node information

Implications:

- A node can answer file-list and search queries from local metadata after synchronization.
- New or recovered nodes must receive the full metadata set.
- The project should not introduce a central metadata service in v1.

### 3. Eventual Consistency

PocketCluster uses eventual consistency.

It does not provide strong consistency in v1.

Implications:

- Nodes may temporarily disagree.
- The system must converge after synchronization.
- UI and conflict handling must tolerate temporary divergence.

### 4. Syncthing-Style Conflict Handling

Conflicting writes must not be silently overwritten.

Use conflict files similar to:

```text
xxx.txt
xxx.conflict.nodeA.timestamp.txt
```

Implications:

- The user may see conflict files.
- Conflict preservation is preferred over automatic data loss.
- Conflict files are normal files in the unified file list.

### 5. Chunk Storage

Files are stored as Chunk objects.

The logical model is:

```text
File
↓
Chunk
↓
SHA256
↓
Replica
```

Implications:

- File metadata records the ordered Chunk list.
- Nodes store Chunk payloads, not only whole-file copies.
- Download reconstructs files from Chunk replicas.

### 6. Chunk Hash Addressing

Chunk ID is content-based:

```text
sha256(chunk)
```

Implications:

- Chunk identity must not depend on device path, upload node, sequence number, or database row ID.
- The same Chunk content should resolve to the same Chunk ID.
- Replica metadata references Chunk IDs.

### 7. Double Replica

v1 uses two replicas for Chunk durability.

Implications:

- Normal placement should keep two copies of each Chunk.
- Replicas should be placed on different nodes when possible.
- Single-node offline scenarios should remain readable when another replica is available.
- Erasure coding is not part of v1.

### 8. Unified Storage Pool Experience

The user-facing product must feel like one storage pool.

It must not feel like browsing several device folders.

Implications:

- Normal mode should focus on files, capacity, upload, download and search.
- Node, Chunk, Replica and sync details belong in advanced mode.
- Implementation convenience must not leak into the core user experience.

## V1 Technical Defaults

These defaults are now sufficient to guide v1 architecture and implementation planning.

### Client Form

v1 uses:

```text
Agent + WebUI
```

- WebUI: React + Vite + Tailwind.
- Desktop Agent: Go for Windows, macOS and future Linux support.
- Android Agent: Kotlin.

### Discovery

v1 uses mDNS for local network discovery.

mDNS is discovery only. It is not an authorization mechanism.

### Cluster Join

v1 uses:

```text
one-time invite code + 10-minute expiry + not node-bound + existing-node approval
```

Invite payload:

```json
{
  "cluster_id": "xxx",
  "bootstrap": "192.168.1.10:7788",
  "join_token": "random-token",
  "expires_at": 1710000000
}
```

Join flow:

```text
new node generates node_id + public_key
↓
user enters invite code
↓
new node connects to bootstrap node
↓
new node submits node_id / public_key / device_info
↓
existing node confirms the request
↓
existing node records the new node in trusted_nodes
↓
new node pulls full metadata
```

After joining, node-to-node requests use signature verification based on trusted public keys.

### Metadata Sync

v1 uses:

```text
Event Log + Snapshot
```

Event identity:

```text
event_id = node_id:seq
```

Each node has its own monotonically increasing `seq`.

v1 event types:

```text
NODE_JOIN
NODE_UPDATE
FILE_PUT
FILE_DELETE
FILE_RENAME
FILE_CONFLICT
CHUNK_REPLICA_ADD
CHUNK_REPLICA_REMOVE
SNAPSHOT_CREATED
```

Ordering rules:

- Same-node events are ordered by `seq`.
- Cross-node events do not have a forced global order.
- File conflicts are resolved through file version and parent version, not global event order.

Retention:

- Event Log is retained for 30 days by default.
- Events older than a Snapshot and acknowledged by all online nodes may be cleaned.
- Long-offline nodes pull Snapshot if required Events were cleaned.

Snapshot creation:

```text
every 1000 events or every 24 hours
```

### File Version And Conflict Detection

v1 does not use vector clocks.

v1 uses:

```text
parent_version_id + node_id + timestamp
```

Version ID:

```text
sha256(file_id + parent_version_id + chunk_ids + node_id + timestamp)
```

Conflict rule:

```text
same path
+ two different versions
+ same parent_version_id
= conflict
```

`node_id:seq` carries local logical order. Pure timestamp is not trusted because device time may be unreliable.

### Chunk Policy

v1 uses:

```text
64MB Chunk size
SHA256 Chunk ID
deduplication allowed
```

Chunk identity remains:

```text
sha256(chunk)
```

### Replica Policy

v1 default replica count is:

```text
2
```

Replica shortage is allowed temporarily.

Status model:

- `healthy`: all chunks have at least 2 replicas.
- `under_replicated`: at least one chunk has fewer than 2 replicas, but the file is still readable.
- `unavailable`: at least one chunk has no online replica.

UI mapping:

- Green: healthy.
- Yellow: under-replicated.
- Red: unavailable.

Repair triggers:

- new node comes online
- offline node recovers
- upload completes with insufficient replicas
- scheduled scan
- user manually clicks repair

Repair priority:

1. `unavailable` risk recovery
2. chunks with replica count = 1
3. recently accessed files
4. small files
5. user-marked important files
6. normal large files

### Android Mode

Android v1 is:

```text
geek mode
```

Requirements:

- Foreground service: required.
- Plugged-in operation: recommended.
- Disable battery optimization: must prompt the user.
- Manual keep-alive: provide vendor-specific guidance where possible.
- Fixed storage directory permission: required.

Android v1 does not promise:

- permanently stable background execution
- full-device file access
- automatic charging management
- consistent behavior across all ROMs

MVP priority is App private directory, for example:

```text
/Android/data/app.package/files/pocketcluster
```

SAF-selected directories such as `Documents/PocketCluster` are later extensions.

### Node Communication

v1 uses:

```text
HTTP REST + JSON
```

v1 does not use gRPC.

Core API surface:

```text
GET  /api/node/info
GET  /api/nodes
POST /api/join/request
POST /api/join/approve
GET  /api/files
POST /api/files/upload
GET  /api/files/download
GET  /api/chunks/{hash}
POST /api/chunks
GET  /api/events?since=nodeA:100
POST /api/events/push
GET  /api/snapshot
```

## Deferred Decisions

The following are intentionally not fixed yet:

- Exact REST request and response schemas.
- Metadata serialization format.
- Search index implementation and whether v1 search includes metadata beyond filename.
- Replica placement scoring details.
- Exact WebUI routing and packaging details.
