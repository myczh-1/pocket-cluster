# Ultimate Goals

## Dream Experience

PocketCluster 的理想体验是：用户把旧手机、旧平板、旧电脑放在家里，安装 Agent 后通过邀请码加入同一个资源池。之后用户看到的是一个统一空间，而不是一组设备。

用户上传文件时，不需要知道文件写入哪台设备；用户下载文件时，也不需要知道哪个节点保存了副本。节点可以上线、离线、恢复，系统在后台完成同步、恢复和冲突处理。普通模式像网盘一样简单，高级模式像家庭基础设施控制台一样透明。

长期来看，PocketCluster 应成为“闲置设备资源池”，存储只是第一能力。未来可以继续扩展到计算、GPU、AI 推理和媒体转码，但 v1 不实现这些能力。

## Long-Term Product Goals

- 将家庭闲置设备组织成一个统一资源池。
- 第一阶段提供可信赖的统一存储体验。
- 让普通用户使用时不需要理解节点、副本和 Chunk。
- 让高级用户可以观察节点状态、副本状态、同步状态和设备健康度。
- 在不引入主节点的前提下，让节点离线、恢复和冲突处理都能被系统吸收。
- 为未来的计算、GPU、AI 推理和媒体转码能力保留扩展空间。

## Future Capabilities To Preserve

v1 不实现下列能力，但架构不应阻塞它们：

- WebDAV 和 SMB 等标准访问协议。
- 自动均衡和自动迁移。
- Android 电池检测与节点能力评估。
- 节点评级和副本策略优化。
- Chunk 可视化和副本分布调试。
- 权限系统。
- 纠删码。
- 内容去重统计。
- 公网节点。
- 计算资源池。
- GPU 调度。
- AI 推理。
- 媒体转码。

## Architecture Implications

### Avoid

- 引入 Leader、Master、Coordinator 或任何必须长期在线的主节点。
- 把元数据只放在某个中心节点或中心数据库中。
- 让文件路径、文件可用性或下载能力依赖原始上传设备。
- 用强一致性协议作为 v1 的核心前提。
- 静默覆盖冲突文件。
- 把 Chunk ID 设计成位置相关、节点相关或自增 ID。
- 把存储能力写死到无法扩展其他资源类型的模型中。
- 让普通模式暴露过多底层概念，破坏统一存储池体验。

### Prefer

- 所有节点平等。
- 每个节点保存完整元数据。
- 元数据采用最终一致模型。
- 冲突采用 Syncthing 式保留多版本处理。
- 文件内容按 Chunk 存储。
- Chunk ID 使用 sha256(chunk)。
- Replica 元数据只描述 Chunk 的副本位置和状态。
- 用户体验始终围绕统一存储池，而不是设备文件夹拼接。
- 高级能力通过可观察状态和策略扩展实现，而不是改变基础模型。

## Explicit Non-Goals

- PocketCluster 不是传统中心化 NAS。
- PocketCluster v1 不是企业级分布式存储系统。
- PocketCluster v1 不追求强一致事务语义。
- PocketCluster v1 不做权限系统。
- PocketCluster v1 不做公网跨地域存储。
- PocketCluster v1 不做计算、GPU、AI 推理或媒体转码。
- PocketCluster v1 不做纠删码。

## Reference Systems To Study Before Implementation

正式开工前应先阅读 Syncthing 和 SeaweedFS 的架构资料，重点看架构图和设计思路，不需要先读源码。

重点关注：

- 设备发现
- 元数据
- 文件索引
- 副本恢复

PocketCluster 的长期形态更接近以下思路的组合：

```text
Syncthing
+
SeaweedFS
+
Home Assistant
```

目标不是从零发明一套新的存储系统，而是在家庭闲置设备场景下组合成熟思路，减少开发量和踩坑概率。
