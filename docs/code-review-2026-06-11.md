# PocketCluster 代码审查报告 (Linus Torvalds 风格)

日期: 2026-06-11
审查范围: 全部 Go 后端源码
审查方法: Linus Torvalds 五层分析法 (数据结构 → 特殊情况 → 复杂度 → 破坏性 → 实用性)

---

## 品味评分: 🟡 凑合

有好品味的骨架——数据结构选择正确、架构决策务实。但实现层有多处"先写后改"留下的坑，部分会在生产环境制造数据损坏或安全事故。

---

## 核心判断

✅ **值得做**。分布式存储是真实问题，去中心化 + chunk 存储 + 事件同步的模型选择正确。

**数据结构**: Node/File/Chunk/Replica 四个核心类型设计合理。事件驱动同步 (event sourcing) 是分布式系统的正确选择。SQLite 作为本地元数据存储务实可靠。

**风险点**: WebDAV 写入路径会 OOM、密码哈希不安全、错误静默忽略会在生产环境制造难以排查的数据不一致。

---

## 致命问题 (P0)

### 1. 密码哈希是纯 SHA256，没有盐

文件: `internal/config/config.go:121-123`

```go
func hashPassword(password string) string {
    h := sha256.Sum256([]byte("pocketcluster:" + password))
    return base64.StdEncoding.EncodeToString(h[:])
}
```

**问题**: SHA256 不是密码哈希函数——没有盐、没有 cost factor。任何拿到 `config.json` 的人可以在毫秒级别暴力破解密码。

**修复**: 用 `golang.org/x/crypto/bcrypt` 或 `argon2`。

```go
import "golang.org/x/crypto/bcrypt"

func hashPassword(password string) string {
    hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    return string(hash)
}
```

### 2. WebDAV 写入把整个文件缓冲到内存

文件: `internal/server/webdav.go:258-336`

```go
type davWriteFile struct {
    buf bytes.Buffer  // 整个文件内容缓冲在这里
}

func (f *davWriteFile) Write(p []byte) (int, error) {
    return f.buf.Write(p)  // 内存无限增长
}

func (f *davWriteFile) Close() error {
    data := f.buf.Bytes()  // 此时整个文件在内存里
    // ... 切 chunk 写入
}
```

**问题**: 一个 2GB 文件通过 WebDAV PUT 写入，会直接 OOM。上传 API (`handleUpload`) 用的是流式 chunk 写入——正确的做法。WebDAV 路径用了 `bytes.Buffer` 全量缓冲——错误的做法。

**修复**: `davWriteFile` 应该边写边切 chunk，类似 `handleUpload` 中的 `io.MultiReader` 模式。`Write()` 方法应该直接调用 `chunk.Storage.Store()` 流式写入，不应该缓冲。

### 3. Store 层重复的 UpsertNode 逻辑

文件: `internal/store/store.go:321-394`

`UpsertNode` (第 321-346 行) 和 `UpdateNodeFull` (第 369-394 行) 有近 50 行几乎相同的 CASE WHEN 语句：

```go
// UpsertNode: 保留旧值模式
name = CASE WHEN excluded.name != '' THEN excluded.name ELSE nodes.name END,
platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE nodes.platform END,
// ... 12 个这样的 CASE WHEN

// UpdateNodeFull: 强制覆盖模式
name = excluded.name,
platform = excluded.platform,
// ... 直接覆盖
```

**问题**: 两个函数做的事情几乎一样，只有"是否保留旧值"的语义差异。这是典型的"特殊情况补丁"。

**修复**: 合并为一个函数加一个 `forceUpdate bool` 参数，或用 options 模式。

### 4. 必须 Marshal 的错误被静默忽略

多处数据库写入错误被 `_ =` 忽略：

```go
// store.go:1173
_ = json.Unmarshal([]byte(candidatesJSON), &n.AddressCandidates)

// server.go:55
json.NewEncoder(w).Encode(v)  // 错误被忽略

// webdav.go:283-285
f.store.UpsertChunk(...)   // 错误被忽略
f.store.UpsertReplica(...) // 错误被忽略
```

**问题**: 数据库写入失败会导致数据不一致。静默忽略是在埋雷——出了问题根本查不到原因。

**修复**: `mustMarshal` 应该返回 error 或在确实不可能失败的场景下 panic。数据库写入错误必须传播。

### 5. davWriteFile.Close() 不做错误检查

文件: `internal/server/webdav.go:266-330`

```go
func (f *davWriteFile) Close() error {
    // ...
    f.store.UpsertChunk(...)   // 返回值被忽略
    f.store.UpsertReplica(...) // 返回值被忽略
    // ...
    f.store.UpsertFile(file)   // 这个检查了，但前面两个没有
    return nil
}
```

**问题**: chunk 和 replica 写入失败时文件记录仍然创建，导致元数据和实际存储不一致。

**修复**: 每个数据库操作的错误都应该检查并传播。

---

## 结构性问题 (P1)

### 6. store.go 1274 行，需要拆分

文件: `internal/store/store.go`

对于一个数据访问层来说太胖了。Node、File、Chunk、Replica、Event、Invite、Snapshot 应该拆分为独立文件。

**建议结构**:
```
store/
  store.go          — Open, Close, migrate, 通用 helper
  nodes.go          — UpsertNode, GetNode, ListNodes...
  files.go          — UpsertFile, GetFile, ListFiles...
  chunks.go         — UpsertChunk, GetChunk...
  replicas.go       — UpsertReplica, GetReplicas...
  events.go         — InsertEvent, GetEvents...
  invites.go        — CreateInvite, UseInvite...
  snapshots.go      — SaveSnapshot, LoadSnapshot...
```

### 7. sync.go 698 行，chunk 搬运逻辑重复

文件: `internal/server/sync.go`

`repairChunkReplicas`、`pushChunkToPeer`、`fetchChunkFromReplica`、`storeChunkToPeer` 这四个函数做的事情高度重叠——它们都在"把 chunk 从 A 搬到 B"，只是方向和细节不同。

**修复**: 统一为 `transferChunk(ctx, from, to, chunkID)` 函数。

### 8. PendingJoin 定义位置错误

文件: `internal/store/store.go:1206-1217`

`PendingJoin` 定义在 `store.go` 里而不是 `types.go`。它被 `server/join.go` 和 `server/handlers_core.go` 都引用。

**问题**: 打破了 store 只管持久化的边界。`PendingJoin` 是一个业务类型，应该在 `types` 包里。

### 9. 事件推送仍然有优化空间

文件: `internal/server/sync.go:204`

```go
events, err := s.store.GetUnpushedEvents(n.NodeID, 1000)
```

虽然用了 `GetUnpushedEvents` (比上次审查的 `GetEventsSince("", 1000)` 有改进)，但每次 sync 仍然可能推 1000 条事件。对于大规模集群，这个数字可能需要调整。

### 10. 部分事件类型仍未处理

文件: `internal/server/events.go`

`applyEvent` 的 `default` 分支直接忽略未识别事件。以下事件类型在远程节点上不被应用：

- `FILE_RENAME`: 重命名不会同步到其他节点
- `FILE_CONFLICT`: 冲突记录不会同步
- `CHUNK_REPLICA_REMOVE`: chunk 移除不会同步

---

## 次要问题 (P2)

### 11. ListFiles 路径过滤用 LIKE

```go
WHERE deleted = 0 AND path LIKE ? AND path NOT LIKE ?
```

`LIKE` 中的 `%` 和 `_` 是通配符。如果路径包含这些字符，查询会返回错误结果。应使用 `=` 前缀匹配或参数化路径。

### 12. SearchFiles 无法利用索引

```go
WHERE deleted = 0 AND name LIKE '%keyword%'
```

前缀 `%` 阻止索引使用。文件数量增长后查询变慢。v1 可接受，但应标记为后续优化点。

### 13. versionID 碰撞风险

文件: `internal/server/handlers_files.go:93`

```go
versionID := uuid.NewString()
```

当前用 UUID，碰撞概率极低。但上次审查提到用 SHA256 拼接的方式——如果回退到那种方式会有问题。当前实现可接受。

---

## 正面评价

- **Event Log + Snapshot 模型**选择正确，event sourcing 是分布式同步的正道
- **Chunk content-addressing** (SHA256) 正确，去重自然发生
- **无主架构**约束被遵守，所有节点对等
- **Ed25519 签名认证**设计合理，签名消息格式清晰
- **peernet.NewHTTPClient** 禁用代理避免局域网流量泄漏，细节到位
- **冲突处理** (`prepareFilePut`) 基本正确，local version 赢得主路径，remote version 变冲突文件
- **SQLite 单连接** (`SetMaxOpenConns(1)`) 避免并发写入问题，适合嵌入式场景
- **mDNS 发现** + **邀请码加入**的双模式设计灵活
- **chunk 写入验证** (`chunk.Storage.Store()` 中的 `Verify()` 调用) 保证数据完整性
- **原子写入** (`os.Rename` 临时文件) 避免写入中断导致损坏

---

## 修复优先级

| 优先级 | 编号 | 问题 | 影响 |
|--------|------|------|------|
| P0 | 1 | 密码 SHA256 无盐 | config.json 泄露 = 密码泄露 |
| P0 | 2 | WebDAV 全量缓冲 | 大文件上传 OOM |
| P0 | 3 | UpsertNode 重复逻辑 | 维护困难，容易引入不一致 |
| P0 | 4 | 错误静默忽略 | 数据不一致难以排查 |
| P0 | 5 | davWriteFile 错误检查 | 元数据和存储不一致 |
| P1 | 6 | store.go 太胖 | 1274 行难以维护 |
| P1 | 7 | sync.go chunk 搬运重复 | 698 行逻辑重叠 |
| P1 | 8 | PendingJoin 位置错误 | 打破分层边界 |
| P1 | 9 | 事件推送可优化 | 大集群性能瓶颈 |
| P1 | 10 | 部分事件未处理 | 重命名/冲突不同步 |
| P2 | 11-13 | 次要代码质量 | 可维护性 |

---

## Linus 式总结

**该做的**: 修复 P0 问题（密码哈希、WebDAV OOM、错误处理），拆分 store.go 和 sync.go。

**不该做的**: 不要为了"理论完美"引入过度抽象。当前的 chunk+replica+event 模型是对的，不要推倒重来。

**下一步**: 先修 P0，再拆文件。拆文件是纯重构，不改行为，风险最低。
