# Product Specification

## Product Overview

PocketCluster 是一个客户端 + Agent 形态的家庭闲置设备资源池产品。第一阶段只实现存储能力：把旧手机、旧平板、旧电脑中的闲置存储空间组成一个无主节点、最终一致的统一存储池。

用户应像使用网盘一样看到统一容量和统一文件列表，而不需要关心文件、Chunk 或副本实际位于哪台设备上。

## Target User

主要用户：具备一定折腾能力的个人用户和极客用户。

他们通常拥有：

- 旧手机
- 旧平板
- 旧电脑
- 多台设备上的闲置存储空间
- 局域网家庭环境

## Usage Scenario

用户在家中希望复用已有设备，而不是额外购买 NAS、硬盘或小主机。

典型场景：

- 第一次把多台设备加入同一个资源池
- 日常查看统一容量、在线节点和文件列表
- 上传文件到资源池
- 从任意在线节点下载文件
- 搜索资源池内文件
- 把本地文件转移到资源池释放当前设备空间
- 在高级模式下排查节点、副本和同步状态

## Product Form

客户端 + Agent。

- Agent：运行在 Windows、Mac、Android 设备上，负责节点发现、加入集群、元数据同步、Chunk 存储、副本维护和文件传输。
- 客户端：面向用户展示统一存储池体验，提供上传、下载、浏览、搜索和状态查看。

客户端与 Agent 可以在同一设备上运行，但产品概念上应保持区分。

## Core User Flow

### First Launch

1. 用户在第一台设备安装 Agent。
2. Agent 自动发现局域网内可加入的 PocketCluster 节点。
3. 用户创建新的资源池。
4. 其他设备通过邀请码加入资源池。
5. 多台设备形成统一存储空间。

### Daily Use

1. 用户打开客户端。
2. 客户端展示总容量、已用容量、在线节点和文件列表。
3. 用户上传、下载、浏览或搜索文件。
4. 用户可将本地文件转移到资源池。
5. 系统在后台完成 Chunk 切分、Hash 寻址、副本生成、元数据同步和离线恢复。

## Core Features

### Feature 1: Node Discovery And Joining

- Description: Agent 通过 mDNS 在局域网内自动发现节点，用户通过邀请码让新设备加入资源池。
- User value: 降低多设备组池门槛，不要求手动配置 IP 或中心服务器。
- Required in v1: yes

### Feature 2: Unified Storage Pool View

- Description: 客户端展示统一容量、已用容量、在线节点和文件列表。
- User value: 用户看到的是一个存储池，而不是多个分散设备。
- Required in v1: yes

### Feature 3: File Upload And Download

- Description: 用户可以上传文件到资源池，也可以从资源池下载文件。
- User value: 完成最基本的网盘式使用闭环。
- Required in v1: yes

### Feature 4: File Browsing And Search

- Description: 用户可以浏览资源池中的文件列表，并按文件名或基础元数据搜索。
- User value: 让统一存储空间可用、可找、可管理。
- Required in v1: yes

### Feature 5: Move Local Files To Pool

- Description: 用户可以将当前设备上的本地文件转移进资源池。
- User value: 释放当前设备空间，同时让文件进入统一访问体系。
- Required in v1: yes

### Feature 6: Advanced Node And Replica Status

- Description: 高级模式展示节点状态、副本状态、同步状态和设备信息。
- User value: 极客用户可以理解系统健康度并排查异常。
- Required in v1: yes

## User Modes

### Normal Mode

普通模式只展示：

- 文件
- 容量
- 上传
- 下载
- 搜索

普通用户不应被 Chunk、副本、同步日志或节点细节打扰。

### Advanced Mode

高级模式可以查看：

- 节点状态
- 副本状态
- 同步状态
- 设备信息

高级模式用于理解和排查，不应成为完成普通文件操作的必要路径。

## MVP Scope

### Included In V1

- Windows Agent
- Mac Agent
- Android Agent
- mDNS 自动发现节点
- 邀请码加入集群
- 节点状态页
- 上传文件
- 下载文件
- 浏览文件
- 搜索文件
- 文件转移到资源池
- Chunk 切分
- Chunk Hash 寻址
- 双副本
- 元数据全量同步
- 节点离线恢复
- Syncthing 式冲突处理

### Excluded From V1

- WebDAV
- SMB
- 自动均衡
- 自动迁移
- Android 电池检测
- 节点评级
- Chunk 可视化
- 权限系统
- 纠删码
- 内容去重统计
- 公网节点
- 计算资源池
- GPU 调度
- AI 推理
- 媒体转码

## Data / Content

v1 需要处理和同步的数据包括：

- 资源池信息
- 节点信息
- 设备信息
- 目录树
- 文件元数据
- Chunk 元数据
- Chunk Hash
- Replica 信息
- 同步状态
- 冲突文件记录

## Fixed Product Decisions

以下决策是当前项目的稳定基础，后续不要轻易改变：

- 无主节点
- 元数据全量同步
- 最终一致
- Syncthing 式冲突处理
- Chunk 存储
- Chunk Hash 寻址
- 双副本
- 统一存储池体验

## Confirmed V1 Technical Defaults

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
- Chunk policy: 64MB, SHA256, deduplication allowed
- Replica policy: default 2 replicas, temporary shortage allowed
- Availability status: `healthy`, `under_replicated`, `unavailable`
- Android mode: geek mode
- Agent communication: HTTP REST + JSON
- gRPC: not used in v1

## Open Questions

- Exact REST request and response schemas.
- Metadata serialization format.
- Search index implementation and whether v1 search includes metadata beyond filename.
- Replica placement scoring details.
- Exact WebUI routing and packaging details.
