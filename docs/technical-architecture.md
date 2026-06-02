# Technical Architecture

## Purpose

This document defines the v1 technical architecture for PocketCluster.

PocketCluster v1 is a no-master, eventually consistent, chunk-based home storage pool. It uses full metadata synchronization, Syncthing-style conflict handling, SHA256-addressed chunks, and two replicas by default.

## Architecture Summary

```text
Agent + WebUI
```

- WebUI: React + Vite + Tailwind.
- Desktop Agent: Go for Windows, macOS and future Linux support.
- Android Agent: Kotlin.
- Discovery: mDNS.
- Join: invite code + mDNS-discovered bootstrap node.
- Node communication: HTTP REST + JSON.
- Metadata sync: Event Log + Snapshot.
- Chunk size: 64MB.
- Chunk ID: `sha256(chunk)`.
- Deduplication: allowed.
- Replica target: 2.
- Under-replication: allowed and surfaced as status.
- Android mode: geek mode.

## Non-Negotiable Constraints

- Do not introduce Leader, Master, Coordinator or any equivalent permanent control node.
- Do not make one node the only metadata authority.
- Do not require strong consistency for v1.
- Do not silently overwrite conflicts.
- Do not use location-based Chunk IDs.
- Do not make the normal user experience feel like browsing multiple device folders.

## Components

### WebUI

The WebUI is the user-facing interface.

Technology:

```text
React + Vite + Tailwind
```

Responsibilities:

- Show total capacity and used capacity.
- Show online and offline nodes.
- Show file list and search results.
- Upload files.
- Download files.
- Move local files into the resource pool.
- Show health status: healthy, under-replicated, unavailable.
- Provide advanced views for node, replica and sync status.

The WebUI should be packageable into the Agent. It may later become a standalone desktop shell without changing the Agent or node protocol.

### Agent

The Agent is the node runtime.

Desktop Agent:

```text
Go: Windows / macOS / Linux
```

Android Agent:

```text
Kotlin: Android
```

Responsibilities:

- Maintain node identity.
- Advertise and discover nodes through mDNS.
- Serve the local WebUI and REST API.
- Handle join request and approval.
- Store full metadata.
- Append and sync Event Log entries.
- Create and serve Snapshots.
- Split files into chunks.
- Store and serve Chunk payloads.
- Track Replica state.
- Repair under-replicated chunks when possible.
- Detect and preserve conflicts.

## Discovery

v1 uses mDNS for local network discovery.

Discovery responsibilities:

- Advertise a node's presence on the local network.
- Publish enough endpoint information for local REST communication.
- Allow a joining node to locate the bootstrap node from an invite code.
- Update online/offline status for known nodes.

mDNS is discovery only. It is not an authorization mechanism.

## Join Protocol

### Join Policy

v1 uses:

```text
one-time invite code + 10-minute expiry + not node-bound + existing-node approval
```

- Validity: 10 minutes.
- Single use: yes.
- Bound to a specific joining node: no.
- Existing node confirmation: required.

### Invite Code Payload

```json
{
  "cluster_id": "xxx",
  "bootstrap": "192.168.1.10:7788",
  "join_token": "random-token",
  "expires_at": 1710000000
}
```

The invite code authorizes a join attempt. It should not be treated as long-term trust.

### Join Flow

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

### Node Trust

The joining node submits:

- `node_id`
- `public_key`
- `device_info`

The approving existing node records the new node in `trusted_nodes`.

After joining, node-to-node requests must be signed and verified with the trusted public key.

## Metadata Model

Every node stores the complete metadata set:

- cluster metadata
- trusted nodes
- node status
- directory tree
- file metadata
- file versions
- chunk metadata
- replica metadata
- event log state
- snapshot state
- conflict records

Metadata is eventually consistent.

Each node must be able to answer file list and search queries from local metadata after synchronization.

## Event Log

v1 uses:

```text
each node has increasing seq
event_id = node_id:seq
```

### Event Identity

- `node_id`: stable node identifier.
- `seq`: monotonically increasing sequence number local to that node.
- `event_id`: `${node_id}:${seq}`.

### Event Types

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

### Ordering Rules

- Events from the same node are ordered by `seq`.
- Events from different nodes do not have a forced global order.
- File conflict detection is resolved through `file version` and `parent_version`, not by global event order.

### Retention

- Event Log is retained for 30 days by default.
- After a Snapshot is generated, events older than that Snapshot and acknowledged by all online nodes may be cleaned.
- If a long-offline node returns after required events were cleaned, it must pull a Snapshot instead of replaying old events.

## Snapshot

v1 uses Snapshot with Event Log.

Snapshot creation policy:

```text
every 1000 events or every 24 hours
```

Snapshot responsibilities:

- Provide a bounded catch-up path for new nodes.
- Provide a bounded recovery path for long-offline nodes.
- Allow old Event Log entries to be cleaned safely.

A Snapshot represents the complete metadata state at a point in logical history.

## File Versioning

v1 does not use vector clocks.

v1 uses:

```text
parent_version_id + node_id + timestamp
```

### Version ID

```text
sha256(file_id + parent_version_id + chunk_ids + node_id + timestamp)
```

Rationale:

- Do not use pure timestamp because device time may be unreliable.
- Do not require Lamport clock in v1.
- Use `node_id:seq` as the local logical order carrier.

## Conflict Detection

Conflict rule:

```text
same path
+ two different versions
+ same parent_version_id
= conflict
```

When a conflict is detected:

- Do not silently overwrite either version.
- Keep the normal file entry.
- Create a Syncthing-style conflict file.

Conflict filename pattern:

```text
xxx.txt
xxx.conflict.nodeA.timestamp.txt
```

The exact suffix may be refined, but it must include enough information to identify the conflicting node and time.

## Chunk Storage

### Chunk Size

v1 Chunk size:

```text
64MB
```

Small files may be represented as a single Chunk.

### Chunk ID

Chunk ID:

```text
sha256(chunk)
```

Chunk ID must not depend on:

- local path
- upload node
- database row ID
- sequence number
- replica location

### Deduplication

Deduplication is allowed because Chunk IDs are content-addressed.

If two files reference the same Chunk hash, the storage layer may store one physical Chunk payload and multiple metadata references.

## Replica Policy

v1 target replica count:

```text
2
```

Replication rules:

- Prefer placing replicas on different nodes.
- Allow uploads when two replicas cannot be created immediately.
- Track under-replication explicitly.
- Repair under-replicated content when eligible nodes become available.

## Availability Status

File or pool health status:

```text
healthy
```

All chunks have at least 2 replicas.

```text
under_replicated
```

At least one chunk has fewer than 2 replicas, but every chunk still has at least one online replica.

```text
unavailable
```

At least one chunk has no online replica.

UI mapping:

- Green: healthy.
- Yellow: under-replicated.
- Red: unavailable.

## Replica Repair

Automatic repair triggers:

- new node comes online
- offline node recovers
- upload completes and replicas are insufficient
- scheduled scan
- user manually clicks repair

Repair priority:

1. `unavailable` risk recovery
2. chunks with replica count = 1
3. recently accessed files
4. small files
5. user-marked important files
6. normal large files

## Android Geek Mode

Android v1 is explicitly geek mode.

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

Storage directory:

- MVP priority: app private directory.
- Example: `/Android/data/app.package/files/pocketcluster`.
- Later extension: SAF-selected directory such as `Documents/PocketCluster`.

## Node Communication Protocol

v1 uses:

```text
HTTP REST + JSON
```

v1 does not use gRPC.

Rationale:

- Easier debugging.
- Easier Android integration.
- WebUI can call APIs directly.
- Local-network v1 performance requirements are acceptable.

If performance pressure becomes obvious later, node-to-node communication may move to gRPC without changing the WebUI contract.

## Core API Surface

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

API details remain to be specified in an API contract document before implementation.

## Implementation Boundary For V1

v1 includes:

- Agent + WebUI.
- mDNS discovery.
- Invite-code join with existing-node approval.
- Trusted node public-key registration.
- Signed node-to-node requests.
- Full metadata sync.
- Event Log + Snapshot.
- 64MB SHA256 chunks.
- Deduplication-capable chunk storage.
- Two-replica target with visible under-replication.
- Conflict preservation.
- Android geek-mode support.

v1 excludes:

- gRPC.
- Strong consistency.
- Vector clocks.
- Erasure coding.
- SMB.
- WebDAV.
- Public internet nodes.
- Full Android background reliability guarantees.
- Full-device Android file access.
