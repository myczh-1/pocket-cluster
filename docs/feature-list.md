# Feature List

## Current v0.1 Snapshot

### Supported

- Windows / macOS / Android agent on reachable local networks
- Invite-based cluster join and pool-level authentication
- Unified pool file upload, download, browsing, and search
- Chunk splitting, SHA256 addressing, dual replicas, metadata sync, and offline recovery
- WebDAV access for standard desktop and Android clients
- Basic advanced diagnostics for node status, replica health, and repair progress

### Experimental / Rough

- Android background reliability
- WebDAV client compatibility breadth
- Health visibility beyond chunk-focused diagnosis
- Operator-triggered jobs (rescan, repair, integrity check) with sync task visibility, still maturing

### Explicitly Not In Current v0.1

- Local file browser and move-to-pool workflow as a primary supported feature
- Public Internet relay or NAT traversal
- Multi-user permissions, ACLs, or share links
- Automatic balancing, erasure coding, and central coordination

## Priority Legend

- P0: Required for v1
- P1: Important but can follow v1
- P2: Later / optional

## P0 Features

### 1. Windows Agent

- User story: 作为拥有闲置 Windows 电脑的用户，我希望把这台电脑加入资源池，用它贡献存储空间。
- Description: Windows Agent 负责节点发现、入池、元数据同步、Chunk 存储和文件传输。
- Acceptance criteria:
  - [ ] Agent 可以在 Windows 上启动并保持运行。
  - [ ] Agent 可以创建或加入资源池。
  - [ ] Agent 可以对外报告节点状态和可用容量。
  - [ ] Agent 可以存储和读取本地 Chunk。

### 2. Mac Agent

- User story: 作为拥有闲置 Mac 的用户，我希望把 Mac 加入同一个资源池。
- Description: Mac Agent 提供与 Windows Agent 等价的节点能力。
- Acceptance criteria:
  - [ ] Agent 可以在 macOS 上启动并保持运行。
  - [ ] Agent 可以创建或加入资源池。
  - [ ] Agent 可以对外报告节点状态和可用容量。
  - [ ] Agent 可以存储和读取本地 Chunk。

### 3. Android Agent

- User story: 作为拥有旧 Android 手机或平板的用户，我希望把它加入资源池贡献存储空间。
- Description: Android Agent 提供节点发现、入池、元数据同步、Chunk 存储和文件传输能力。
- Acceptance criteria:
  - [ ] Agent 可以在 Android 设备上启动。
  - [ ] Agent 可以加入已有资源池。
  - [ ] Agent 可以对外报告节点状态和可用容量。
  - [ ] Agent 可以存储和读取本地 Chunk。

### 4. mDNS Node Discovery

- User story: 作为用户，我希望设备自动发现局域网内的其他节点，不需要手动输入 IP。
- Description: Agent 使用 mDNS 发现同一局域网内的 PocketCluster 节点。
- Acceptance criteria:
  - [ ] 新启动节点可以广播自身存在。
  - [ ] 已有节点可以发现新节点。
  - [ ] 节点离线后状态可以从在线变为离线。
  - [ ] 发现机制不依赖 Leader、Master 或 Coordinator。

### 5. Invite-Code Cluster Join

- User story: 作为用户，我希望通过邀请码把新设备加入资源池。
- Description: 已在池内的设备生成邀请码，新设备通过邀请码和 mDNS 发现完成入池流程。
- Acceptance criteria:
  - [ ] 已有设备可以生成邀请码。
  - [ ] 新设备输入邀请码后可以获得加入授权。
  - [ ] 新设备通过 mDNS 发现可加入节点。
  - [ ] 新设备加入后出现在节点列表中。
  - [ ] 加入流程不要求配置中心节点。

### 6. Node Status Page

- User story: 作为高级用户，我希望查看每个节点的在线状态、容量和同步状态。
- Description: 客户端展示资源池中的节点状态。
- Acceptance criteria:
  - [ ] 页面展示节点名称、平台、在线状态和可用容量。
  - [ ] 页面展示节点最近同步状态。
  - [ ] 离线节点不会导致整个资源池不可用。

### 7. File Upload

- User story: 作为用户，我希望把文件上传到统一资源池。
- Description: 上传文件后，系统自动切 Chunk、计算 SHA256、写入元数据并生成双副本。

Scope note: 普通上传是网盘式上传，只把用户选择的文件内容写入资源池；它不删除、不移动、也不管理用户本地原文件。
- Acceptance criteria:
  - [ ] 用户可以选择本地文件上传。
  - [ ] 上传后文件出现在统一文件列表中。
  - [ ] 文件被切分为 Chunk。
  - [ ] 每个 Chunk 使用 sha256(chunk) 作为 Chunk ID。
  - [ ] 每个 Chunk 在可行情况下生成两个副本。

### 8. File Download

- User story: 作为用户，我希望从任意在线节点下载资源池中的文件。
- Description: 客户端根据元数据定位 Chunk，从可用副本读取并重组文件。
- Acceptance criteria:
  - [ ] 用户可以从文件列表选择文件下载。
  - [ ] 下载文件内容与上传文件一致。
  - [ ] 当一个副本所在节点离线时，可以从另一个副本读取。
  - [ ] 下载流程不要求访问原始上传设备。

### 9. File Browsing

- User story: 作为用户，我希望像使用网盘一样浏览资源池文件。
- Description: 客户端基于同步后的完整元数据展示目录树和文件列表。
- Acceptance criteria:
  - [ ] 客户端展示目录树。
  - [ ] 客户端展示文件名、大小和基础时间信息。
  - [ ] 任意节点看到的文件列表最终一致。

### 10. File Search

- User story: 作为用户，我希望快速找到资源池中的文件。
- Description: v1 至少支持按文件名搜索。
- Acceptance criteria:
  - [ ] 用户可以输入关键词搜索文件名。
  - [ ] 搜索结果来自统一元数据，而不是单台设备的本地目录。
  - [ ] 节点同步完成后，搜索结果最终一致。

### 11. WebDAV

- User story: 作为用户，我希望通过标准 WebDAV 客户端访问资源池。
- Description: 提供 WebDAV 兼容访问层，供 Finder、Windows 资源管理器和 Android 文件管理器挂载。
- Acceptance criteria:
  - [ ] WebDAV 客户端可以浏览文件。
  - [ ] WebDAV 客户端可以上传和下载文件。
  - [ ] 覆盖已有文件时支持基于 ETag 的条件写入保护。

### 12. Advanced Health Summary

- User story: 作为高级用户，我希望快速知道当前文件和副本是否安全。
- Description: 提供健康汇总、Chunk 详情和修复进度，让用户判断当前副本覆盖与风险状态。
- Acceptance criteria:
  - [ ] 页面展示整体 `healthy / under_replicated / unavailable / repairing` 状态。
  - [ ] 页面展示受影响文件数量和相关 Chunk 信息。
  - [ ] 页面展示基础修复进度与最近扫描时间。

### 13. Chunk Splitting

- User story: 作为系统，我需要把文件拆分为 Chunk，以便跨设备存储和复制。
- Description: 文件上传时被切分为 Chunk，并记录 Chunk 顺序和大小。
- Acceptance criteria:
  - [ ] 同一文件可被拆分为一个或多个 Chunk。
  - [ ] 元数据记录文件到 Chunk 的顺序映射。
  - [ ] 文件可由 Chunk 顺序重组。

### 14. Chunk Hash Addressing

- User story: 作为系统，我需要用内容 Hash 定位 Chunk，避免依赖设备本地路径。
- Description: Chunk ID 固定为 sha256(chunk)。
- Acceptance criteria:
  - [ ] 相同内容的 Chunk 产生相同 Chunk ID。
  - [ ] 不同内容的 Chunk 产生不同 Chunk ID。
  - [ ] Replica 元数据通过 Chunk ID 引用内容。

### 15. Double Replica

- User story: 作为用户，我希望单个节点离线时文件仍可读取。
- Description: 每个 Chunk 在不同节点上保持两个副本。
- Acceptance criteria:
  - [ ] 正常情况下每个 Chunk 有两个副本。
  - [ ] 两个副本优先分布在不同节点。
  - [ ] 单个副本节点离线时文件仍可读取。
  - [ ] 副本不足时系统能标记风险状态。

### 16. Full Metadata Sync

- User story: 作为系统，我需要每个节点保存完整元数据，避免中心元数据服务。
- Description: 每个节点保存完整目录树、文件信息、Chunk 信息和节点信息，并进行全量同步。
- Acceptance criteria:
  - [ ] 每个节点保存完整目录树。
  - [ ] 每个节点保存完整文件、Chunk 和 Replica 元数据。
  - [ ] 新节点加入后可以获得完整元数据。
  - [ ] 元数据同步不依赖 Leader、Master 或 Coordinator。

### 17. Offline Recovery

- User story: 作为用户，我希望节点离线后恢复上线时自动追上变化。
- Description: 节点离线期间其他节点继续使用；节点恢复后同步缺失元数据和需要的副本状态。
- Acceptance criteria:
  - [ ] 节点离线不会阻塞其他节点上传和下载。
  - [ ] 节点恢复后能同步离线期间的元数据变化。
  - [ ] 节点恢复后能更新本地副本状态。
  - [ ] 恢复过程最终收敛到一致文件列表。

### 18. Syncthing-Style Conflict Handling

- User story: 作为用户，我希望冲突不会导致数据被静默覆盖。
- Description: 当同一路径出现并发修改冲突时，保留原文件并生成带节点和时间戳的 conflict 文件。
- Acceptance criteria:
  - [ ] 冲突发生时不静默覆盖任一版本。
  - [ ] 冲突文件命名包含 conflict、节点标识和时间戳。
  - [ ] 冲突文件出现在统一文件列表中。
  - [ ] 用户可以下载或处理冲突文件。

## P1 Features

### 1. Local File Browser And Move To Pool

- User story: 作为用户，我希望先查看当前节点本机磁盘上的文件，再选择其中一部分迁移进资源池释放当前设备空间。
- Description: 这是独立于普通上传的本节点本地文件管理工作流。只有在资源池写入、副本生成和元数据同步确认完成后，才允许进入删除本地原文件或用户确认删除流程。
- Acceptance criteria:
  - [ ] 用户可以浏览当前节点本机文件列表。
  - [ ] 用户可以从本机文件列表选择文件执行迁移到资源池。
  - [ ] 迁移失败时本地原文件不丢失。
  - [ ] 普通上传流程不会删除或移动本地原文件。

### 2. Automatic Balancing

- User story: 作为用户，我希望系统自动平衡各节点存储压力。
- Description: 根据容量和副本分布移动 Chunk 副本。
- Acceptance criteria:
  - [ ] 系统可以识别明显不均衡的节点容量。
  - [ ] 系统可以在不破坏双副本的前提下调整副本位置。

### 3. Automatic Migration

- User story: 作为用户，我希望节点空间不足或准备下线时系统自动迁移数据。
- Description: 将副本从风险节点迁移到其他节点。
- Acceptance criteria:
  - [ ] 用户可以标记节点准备退出。
  - [ ] 系统可以为该节点上的 Chunk 创建替代副本。

### 4. Android Battery Detection

- User story: 作为 Android 用户，我希望系统避免在低电量时过度消耗设备。
- Description: Android Agent 根据电池状态调整后台行为。
- Acceptance criteria:
  - [ ] Agent 可以读取电池状态。
  - [ ] 低电量时可以暂停重负载副本任务。

### 5. Node Scoring

- User story: 作为高级用户，我希望看到节点可靠性评分。
- Description: 根据在线时间、容量、性能和电量等因素评估节点适合承担副本的程度。
- Acceptance criteria:
  - [ ] 节点评分可见。
  - [ ] 副本策略可以参考评分。

## P2 Features

### 1. SMB

- User story: 作为用户，我希望通过 SMB 挂载资源池。
- Description: 提供 SMB 兼容访问层。
- Reason for postponing: 协议复杂度高，非 v1 统一存储体验的最小必要条件。

### 2. Chunk Visualization

- User story: 作为高级用户，我希望可视化查看文件 Chunk 和副本分布。
- Description: 展示 Chunk、Hash 和副本所在节点。
- Reason for postponing: 对调试有帮助，但普通使用不需要。

### 3. Permission System

- User story: 作为多用户场景用户，我希望控制不同用户访问权限。
- Description: 引入用户、权限和访问控制。
- Reason for postponing: v1 面向个人和家庭极客场景，权限系统会显著扩大复杂度。

### 4. Erasure Coding

- User story: 作为高级用户，我希望用更低冗余成本获得容错能力。
- Description: 用纠删码替代或补充双副本。
- Reason for postponing: v1 固定采用双副本，纠删码会增加恢复和一致性复杂度。

### 5. Deduplication Statistics

- User story: 作为用户，我希望知道内容寻址带来的去重收益。
- Description: 展示相同 Chunk 带来的空间节省。
- Reason for postponing: 统计不是 v1 可用性的必要条件。

### 6. Public Internet Nodes

- User story: 作为用户，我希望不在同一局域网的设备也能加入资源池。
- Description: 支持公网或跨网络节点连接。
- Reason for postponing: NAT、认证、安全和可靠性问题会显著扩大 v1 范围。
