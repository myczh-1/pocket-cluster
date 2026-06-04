# PocketCluster 代码审查报告

日期: 2026-06-03
审查范围: 全部源码 (Go Agent + React WebUI + 文档)

---

## 品味评分: 🟡 凑合

数据结构基本正确，架构决策合理。但实现层有若干"先写后改"留下的坑，部分会导致生产环境 panic 或数据损坏。

---

## 致命问题 (P0)

### 1. `detectMime` 会 panic

文件: `internal/server/server.go:42-58`

```go
func detectMime(path string) string {
    ext := path[len(path)-4:]  // panic: 文件名<4字符直接崩
    switch ext {
    case ".jpg", "jpeg":  // jpeg 缺少前导点号
        return "image/jpeg"
    ...
```

- 文件名长度 < 4 时直接 panic（如 `a.go`、`x`、`Makefile`）
- `"jpeg"` case 不带前导点号，永远无法匹配
- 标准库已解决: `mime.TypeByExtension(filepath.Ext(path))`

**修复**: 删除此函数，用标准库替代。

### 2. 上传处理分配 64MB 临时缓冲区 + 绕过原子写入

文件: `internal/server/handlers_files.go:39-57`

```go
chunkBuf := make([]byte, chunk.ChunkSize)  // 每次循环分配 64MB
n, readErr := io.ReadFull(file, chunkBuf)
...
os.WriteFile(chunkPath, chunkBuf[:n], 0o644)  // 非原子写入
```

问题:
- 每次 chunk 循环迭代分配 64MB，上传 256MB 文件 = 4 次 64MB 分配
- 绕过 `chunk.Storage.Store()` 的 temp-file + atomic-rename 模式
- `os.WriteFile` 非原子写入，中途崩溃留下损坏 chunk

**修复**: 使用 `io.LimitReader` 包装上传流，复用单个 buffer，或者直接调用 `chunk.Storage.Store()` 多次。

### 3. `repairChunkReplicas` 吞掉错误

文件: `internal/server/sync.go:302-303`

```go
if err := s.fetchChunkFromReplica(ctx, chunkID); err != nil {
    return nil  // BUG: 应该 return err
}
```

fetch 失败时返回 nil，上层认为修复成功。缺失副本的 chunk 不会被重试。

**修复**: `return nil` → `return err`。

### 4. 下载中途出错产生损坏响应

文件: `internal/server/handlers_files.go:162-169`

```go
for _, chunkID := range f.ChunkIDs {
    if err := s.writeChunk(r.Context(), w, chunkID); err != nil {
        writeError(w, http.StatusNotFound, ...)  // 已发送部分二进制流，再写 JSON = 损坏
        return
    }
}
```

第一个 chunk 成功写入后 HTTP headers 已发送。后续 chunk 失败时 `writeError` 会在二进制流后追加 JSON。

**修复**: 写入前预检查所有 chunk 可用性；写入后失败只 log，断开连接。

### 5. `handleJoinApprove` 是空壳

文件: `internal/server/handlers_core.go:240-242`

```go
func (s *Server) handleJoinApprove(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, types.APIResponse{OK: true, ...})  // 永远返回 true
}
```

永远返回 `approved: true`。auto-discovery 模式下任何能发 HTTP 的设备都能加入集群。

**修复**: 实现真实审批逻辑，或在不需要审批的模式下返回 404。

---

## 结构性问题 (P1)

### 6. 事件推送是全量广播

文件: `internal/server/sync.go:160`

```go
events, err := s.store.GetEventsSince("", 1000)  // since="" = 从头推
```

每次 sync（每 2 秒）向每个 peer 推送最多 1000 条事件。无增量跟踪。O(n²) 网络开销。

**修复**: 维护 per-peer last-seen event_id 映射，只推增量。

### 7. 版本 ID 可碰撞

文件: `internal/server/handlers_files.go:93`

```go
versionID := fmt.Sprintf("%x", sha256.Sum256([]byte(
    fileID + strings.Join(chunkIDs, ",") + s.cfg.NodeID + fmt.Sprint(now.UnixNano()))))
```

`UnixNano()` 在高频上传时可能重复。fileID 已是 UUID，其他字段附加熵有限。

### 8. multipart 内存限制过高

文件: `internal/server/handlers_files.go:20`

```go
r.ParseMultipartForm(1 << 30)  // 1GB
```

恶意客户端可发送 1GB multipart 请求耗尽内存。应配合 `http.MaxBytesReader` 使用。

### 9. Snapshot 未实现

文档定义了 `GET /api/snapshot` 和 Snapshot 创建策略（每 1000 事件或 24 小时），但代码中无 Snapshot 创建或消费逻辑。Event Log 30 天保留期后，新节点无法获取完整元数据。

### 10. 部分事件类型未处理

文件: `internal/server/events.go:71-72`

`applyEvent` 的 `default` 分支直接忽略未识别事件。以下事件类型未被远程节点应用:

- `FILE_RENAME`: 重命名不会同步到其他节点
- `FILE_CONFLICT`: 冲突记录不会同步
- `CHUNK_REPLICA_REMOVE`: chunk 移除不会同步

---

## 次要问题 (P2)

### 11. `store.go` 迁移用字符串匹配检测错误

```go
func isDuplicateColumnError(err error) bool {
    return strings.Contains(err.Error(), "duplicate column name")
}
```

依赖 SQLite 错误消息文本，不同版本可能变化。应改用 `CREATE TABLE IF NOT EXISTS` 模式或显式检查列是否存在。

### 12. `ListFiles` 路径过滤用 LIKE

```go
WHERE deleted = 0 AND path LIKE ? AND path NOT LIKE ?
```

`LIKE` 中的 `%` 和 `_` 是通配符。如果路径包含这些字符，查询会返回错误结果。应使用 `=` 前缀匹配或参数化路径。

### 13. `SearchFiles` 无法利用索引

```go
WHERE deleted = 0 AND name LIKE '%keyword%'
```

前缀 `%` 阻止索引使用。文件数量增长后查询变慢。v1 可接受，但应标记为后续优化点。

### 14. `RingBuffer` 全局变量

```go
var LogRing *RingBuffer  // 包级全局变量
```

通过全局变量连接 `main` 和 `server`，增加了隐式耦合。应通过依赖注入传递。

---

## 正面评价

- **Event Log + Snapshot 模型**选择正确，`event_id = node_id:seq` 简单且足够
- **Chunk content-addressing** (SHA256) 正确，去重自然发生
- **无主架构**约束被遵守，所有节点对等
- **Ed25519 签名认证**设计合理，签名消息格式清晰
- **peernet.NewHTTPClient** 禁用代理避免局域网流量泄漏，细节到位
- **冲突处理** (`prepareFilePut`) 基本正确，local version 赢得主路径，remote version 变冲突文件
- **SQLite 单连接** (`SetMaxOpenConns(1)`) 避免并发写入问题，适合嵌入式场景
- **mDNS 发现** + **邀请码加入**的双模式设计灵活

---

## 修复优先级

| 优先级 | 编号 | 问题 | 影响 |
|--------|------|------|------|
| P0 | 1 | detectMime panic | 短文件名导致进程崩溃 |
| P0 | 2 | 上传 64MB buffer + 非原子写入 | 内存压力 + chunk 损坏 |
| P0 | 3 | repairChunkReplicas 吞错误 | 副本丢失不被察觉 |
| P0 | 4 | 下载中途错误损坏响应 | 客户端收到损坏文件 |
| P0 | 5 | handleJoinApprove 空壳 | 未授权节点可加入 |
| P1 | 6 | 事件全量广播 | O(n²) 网络开销 |
| P1 | 8 | multipart 1GB 内存限制 | OOM 风险 |
| P1 | 9 | Snapshot 未实现 | 30天后新节点无法加入 |
| P1 | 10 | 部分事件类型未处理 | 重命名/冲突不同步 |
| P2 | 7 | 版本 ID 碰撞 | 极低概率但存在 |
| P2 | 11-14 | 次要代码质量 | 可维护性 |
