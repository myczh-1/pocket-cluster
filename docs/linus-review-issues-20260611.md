# PocketCluster 代码品味审查 — 问题清单

**审查日期**: 2026-06-11
**审查标准**: Linus Torvalds "Good Taste" 哲学
**代码总量**: ~8,700 行 Go（不含测试）

---

## 🔴 严重问题（必须修复）

### 1. `sync.go` — 698 行，职责混乱

**文件**: `internal/server/sync.go` (698 行)
**问题**: 一个文件混合了 4 个不同职责，嵌套过深

**包含的职责**:
- 事件推送/拉取 (`pushEvents`, `pullEvents`)
- 分块修复 (`repairChunkReplicas`, `fetchMissingChunks`)
- 分块远程存储/获取 (`storeChunkToPeer`, `storeRemoteChunk`, `fetchChunkFromReplica`)
- 副本状态管理 (`replicaStatusForChunks`, `onlineNodeSet`)

**具体坏品味证据**:

`fetchMissingChunks()` (第 269-331 行) 是 62 行的函数，4 层嵌套：
```
for _, f := range files {           // 第1层：遍历文件
    for _, chunkID := range f.ChunkIDs {  // 第2层：遍历chunk
        if s.chunks.Exists(chunkID) {     // 第3层：检查本地
            // ...
        }
        for _, r := range replicas {      // 第4层：检查远程副本
            // ...
        }
    }
}
```

**修复方向**: 拆成 4 个文件：
- `sync_events.go` — 事件推送/拉取
- `sync_chunks.go` — 分块远程存储/获取
- `sync_repair.go` — 分块修复逻辑
- `sync_status.go` — 副本状态管理

---

### 2. `Node` 和 `NodeRef` 重复定义

**文件**: `internal/types/types.go` (第 8-23 行, 第 142-156 行)
**问题**: 两个结构体 15 个字段中有 13 个完全相同

```go
type Node struct {        // 15 fields
    NodeID, Name, Platform, Address string
    AddressCandidates []string
    LastWorkingAddress string
    PublicKey string
    TotalBytes, UsedBytes, AvailableBytes int64
    Status string
    Trusted bool
    LastSeen, JoinedAt time.Time
}

type NodeRef struct {     // 15 fields, 13 identical to Node
    // ...完全相同的字段列表...
}
```

**导致的代码浪费**: `handlers_core.go` 中出现了两次完全相同的 13 行字段拷贝：
- 第 207-224 行 (`handleJoinRequest`)
- 第 363-380 行 (`handleJoinApprove`)

**修复方向**: 删除 `NodeRef`，在 `Node` 上实现 `ToRef()` 方法，或用 JSON tag 控制序列化字段。

---

### 3. 上传处理器的字节级读取 hack

**文件**: `internal/server/handlers_files.go` (第 86-98 行)
**问题**: 读取 1 字节，再用 MultiReader 重新拼接

```go
var first [1]byte
for {
    n, readErr := file.Read(first[:])
    // ...
    hash, size, err := s.chunks.Store(io.MultiReader(
        bytes.NewReader(first[:]),
        io.LimitReader(file, chunk.ChunkSize-1),
    ))
}
```

**为什么会这样**: `chunk.Storage.Store()` 没有提供按大小限制读取的接口，所以处理器用 hack 来手动分块。

**修复方向**: 重新设计 `chunk.Storage` 接口，让 `Store()` 接受 `io.Reader` + `maxSize` 参数，内部处理分块边界。

---

## 🟡 中等问题（应该修复）

### 4. Schema 迁移无事务保护

**文件**: `internal/store/store.go` (第 71-74 行)
**问题**: 两个独立的 SQL 操作没有事务包裹

```go
if current < schemaVersion {
    s.db.Exec(`DELETE FROM schema_version`)      // 第1步
    s.db.Exec(`INSERT INTO schema_version ...`)  // 第2步
}
```

**风险**: 如果第 1 步成功但第 2 步失败，schema_version 表为空，下次启动时所有迁移重新执行。其中 `clearLoopbackAddresses()` (V2 迁移) 会清空所有节点地址，生产环境下是灾难性的。

**修复方向**: 用 `db.Begin()` 事务包裹这两行操作。

---

### 5. 认证中间件硬编码路由

**文件**: `internal/server/auth.go` (第 21-61 行)
**问题**: 路由判断逻辑用硬编码字符串匹配

```go
if r.URL.Path == "/api/health" || r.URL.Path == "/api/join/request" {
    // ...
}
if r.URL.Path == "/api/join" {
    // ...
}
if r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/status" {
    // ...
}
```

**问题**: 每增加一个新路由，都必须在这个函数里加一行 if 判断。容易遗漏，维护成本高。

**修复方向**: 用路由表或 set 来管理需要/不需要认证的路径。

---

### 6. 事件类型用字符串常量而非枚举

**文件**: `internal/types/types.go` (第 56-71 行)
**问题**: `EventType` 是 `string` 类型，常量值是字符串字面量

```go
type EventType string

const (
    EventNodeJoin    EventType = "NODE_JOIN"
    EventFilePut     EventType = "FILE_PUT"
    // ...
)
```

**问题**: 编译器无法检查拼写错误。写成 `EventFilePat` 不会报错，只会在运行时静默失败。

**修复方向**: 虽然 Go 没有原生枚举，但可以用 `iota` + `String()` 方法，或在关键入口点加 `switch` 校验。

---

### 7. `store.go` 334 行偏大

**文件**: `internal/store/store.go` (334 行)
**问题**: 包含迁移逻辑 + 扫描器函数 + 辅助函数，职责偏多

**当前内容分布**:
- 迁移逻辑 (第 1-263 行): `migrate()`, `migrateV1-V5`, `addColumnIfMissing`, `columnExists`, `seedFileSearchIndex`, `clearLoopbackAddresses`
- 扫描器 (第 265-334 行): `scanNode`, `scanFile`, `scanNodeRows`, `scanFileRows`
- 辅助函数: `timeMillis`, `boolToInt`, `intToBool`

**修复方向**: 把迁移逻辑拆到 `migrate.go`，扫描器拆到 `scanners.go`。

---

### 8. `isChunkReadable` 重复远程探测逻辑

**文件**: `internal/server/sync.go` (第 546-589 行)
**问题**: 和 `writeChunk` (第 591-636 行) 有大量重复的"遍历副本 → 检查在线 → 尝试 HTTP 请求"模式

```go
// isChunkReadable 的核心循环
for _, replica := range replicas {
    n, err := s.store.GetNode(replica.NodeID)
    for _, address := range nodeDialAddresses(*n) {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, ...)
        resp, err := peerHTTPClient.Do(req)
        // ...
    }
}

// writeChunk 的核心循环 — 几乎相同
for _, replica := range replicas {
    n, err := s.store.GetNode(replica.NodeID)
    for _, address := range nodeDialAddresses(*n) {
        url := "http://" + address + "/api/chunks/" + chunkID
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        resp, err := peerHTTPClient.Do(req)
        // ...
    }
}
```

**修复方向**: 提取一个 `forEachAvailableReplica(ctx, chunkID, fn)` 方法，消除重复。

---

## 🟢 轻微问题（建议改进）

### 9. `chunk.Storage.Open()` 返回 `*os.File` 而非接口

**文件**: `internal/chunk/chunk.go` (第 63-75 行)
**问题**: 返回具体类型 `*os.File`，调用者必须手动 `defer f.Close()`

**建议**: 返回 `io.ReadCloser` 或自定义 `ChunkReader` 类型，把资源管理封装在内部。

---

### 10. `Server` 结构体缺少 health 字段初始化

**文件**: `internal/server/server.go` (第 15-25 行)
**问题**: `health` 字段在 `Server` 结构体中声明，但在 `New()` 函数中没有初始化

```go
type Server struct {
    // ...
    health *healthScanner  // 声明了
}

func New(...) *Server {
    srv := &Server{cfg: cfg, store: s, chunks: c, ...}  // 没有初始化 health
    // ...
}
```

**影响**: `sync.go` 第 89 行用 `s.health != nil` 来判断是否启用健康扫描。这意味着健康扫描默认关闭，需要额外的初始化步骤。这不是 bug，但不够显式。

---

### 11. `handleJoinRequest` 函数过长

**文件**: `internal/server/handlers_core.go` (第 192-287 行)
**问题**: 95 行的函数，处理了 3 种不同的加入场景：
1. 已批准节点重试（第 203-233 行）
2. 待审批节点查询（第 235-242 行）
3. 新节点提交加入请求（第 244-287 行）

**建议**: 拆成 3 个私有方法，每个处理一种场景。

---

## 问题汇总

| # | 严重度 | 文件 | 问题 | 行数影响 |
|---|--------|------|------|----------|
| 1 | 🔴 严重 | sync.go | 698 行，4 个职责混合 | 需拆分 |
| 2 | 🔴 严重 | types.go | Node/NodeRef 重复定义 | 40 行冗余 |
| 3 | 🔴 严重 | handlers_files.go | 字节级读取 hack | 接口设计问题 |
| 4 | 🟡 中等 | store.go | Schema 迁移无事务保护 | 2 行 |
| 5 | 🟡 中等 | auth.go | 认证路由硬编码 | 维护成本 |
| 6 | 🟡 中等 | types.go | 事件类型无编译期校验 | 类型安全 |
| 7 | 🟡 中等 | store.go | 334 行偏大 | 建议拆分 |
| 8 | 🟡 中等 | sync.go | 重复的远程探测逻辑 | ~60 行重复 |
| 9 | 🟢 轻微 | chunk.go | Open() 返回具体类型 | 接口设计 |
| 10 | 🟢 轻微 | server.go | health 字段初始化不显式 | 可读性 |
| 11 | 🟢 轻微 | handlers_core.go | handleJoinRequest 过长 | 95 行 |

---

**结论**: 核心架构品味良好，但同步层和部分处理器的复杂性需要整理。这是一个**可以修复的项目**，不是需要重写的项目。优先处理 #1、#2、#3 三个严重问题，其余可逐步改进。
