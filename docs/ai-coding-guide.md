# AI Coding Guide

## Project Summary

PocketCluster 是一个无主节点的家庭闲置设备资源池。v1 只实现第一能力：存储。

目标是让 Windows、Mac、Android 三类设备组成统一存储池。用户像使用网盘一样上传、下载、浏览和搜索文件，而不需要关心文件实际位于哪台设备。

## Development Principle

优先保证核心模型稳定，不要为了短期实现方便破坏项目的长期方向。

核心体验是：

```text
统一存储空间
```

不是：

```text
多个设备目录的聚合视图
```

## Current Priority

下一阶段应先完成正式技术方案设计，而不是直接写业务代码。

建议顺序：

1. 阅读 Syncthing 和 SeaweedFS 的架构资料。
2. 明确节点发现、入池、身份、元数据同步、Chunk 存储、副本恢复的技术方案。
3. 再开始拆分 Agent、客户端、协议和存储模块。

## Source Of Truth

The following documents should be treated as the project source of truth:

- docs/project-goal.md
- docs/product-spec.md
- docs/feature-list.md
- docs/ultimate-goals.md
- docs/architecture-decisions.md
- docs/technical-architecture.md

## Fixed Decisions

以下决策已经基本定死，后续不要轻易改变：

- 无主节点
- 元数据全量同步
- 最终一致
- Syncthing 式冲突处理
- Chunk 存储
- Chunk Hash 寻址
- 双副本
- 统一存储池体验

## Rules For AI Assistants

- Do not start coding before reading the source-of-truth documents.
- Do not expand the project scope without explicit user confirmation.
- Always distinguish v1 requirements from later ideas.
- Do not simplify v1 in a way that blocks the long-term goals described in docs/ultimate-goals.md.
- If requirements are unclear, ask before implementing.
- Prefer small, reviewable changes.
- Update documentation when product decisions change.
- Do not introduce Leader, Master, Coordinator, or any equivalent permanent control node.
- Do not implement v1 as a center-server architecture.
- Do not store metadata only on one node.
- Do not use strong consistency as a hidden requirement.
- Do not silently overwrite conflicting files.
- Do not use location-based Chunk IDs.
- Do not expose Chunk and Replica concepts in normal user flows unless necessary.

## Metadata Rules

Every node stores the complete metadata set:

- directory tree
- file information
- Chunk information
- Replica information
- node information

Metadata synchronization uses eventual consistency.

Do not design metadata around a single authoritative node.

## File Storage Rules

The storage model is:

```text
File
↓
Chunk
↓
SHA256
↓
Replica
```

Chunk ID must be:

```text
sha256(chunk)
```

Replica records should point from Chunk ID to the nodes that currently store that Chunk.

## Consistency Rules

Use:

```text
Eventual Consistency
```

Do not use:

```text
Strong Consistency
```

The product should tolerate temporary divergence and converge after synchronization.

## Conflict Handling Rules

Use the Syncthing model.

When two nodes produce conflicting versions of the same logical file, do not silently choose a winner. Preserve conflict versions with filenames like:

```text
xxx.txt
xxx.conflict.nodeA.timestamp.txt
```

The exact conflict suffix may be refined later, but it must include enough information to identify the conflicting node and time.

## MVP Target

Implement a three-node resource pool across:

- Windows
- Mac
- Android

After uploading a file:

- the file is automatically split into Chunk objects
- each Chunk is addressed by SHA256 hash
- replicas are automatically generated
- any node can access the file through unified metadata
- the file remains readable when one node is offline if another replica exists
- recovered nodes automatically synchronize metadata and replica state

## V1 Technical Defaults

- Client form: Agent + WebUI
- WebUI stack: React + Vite + Tailwind
- Desktop Agent: Go for Windows and macOS
- Android Agent: Kotlin
- Discovery: mDNS
- Cluster join: one-time invite code, 10-minute expiry, not node-bound, existing-node approval required
- Node trust: new node submits `node_id`, `public_key` and `device_info`; approved nodes enter `trusted_nodes`
- Node authentication: later node-to-node requests use signature verification
- Metadata sync: Event Log + Snapshot
- Event identity: `event_id = node_id:seq`
- Snapshot policy: every 1000 Events or every 24 hours
- Conflict detection: same path, different versions, same `parent_version_id`
- File version: `parent_version_id + node_id + timestamp`
- Chunk size: 64MB
- Chunk ID: SHA256, specifically `sha256(chunk)`
- Deduplication: allowed
- Replica count: default 2
- Under-replication: allowed temporarily, but must be visible in metadata/status
- Availability status: `healthy`, `under_replicated`, `unavailable`
- Android mode: geek mode; foreground service required, battery optimization prompt required, app private directory first
- Agent communication: HTTP REST + JSON
- gRPC: not used in v1

## Recommended Next Steps

1. Study Syncthing architecture for device discovery, index exchange and conflict handling.
2. Study SeaweedFS architecture for volume/chunk placement and recovery ideas.
3. Define exact REST request and response schemas.
4. Define the metadata serialization format.
5. Define the first implementation milestone from docs/technical-architecture.md.
