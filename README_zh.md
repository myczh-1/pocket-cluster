# PocketCluster

[English](README.md)

> **早期 MVP 版本** — PocketCluster 仍在积极开发中，很多功能还比较简陋，API 随时可能变动。暂不建议用于生产环境。

把闲置的手机、电脑、平板变成一个统一存储池。

不需要 NAS，不需要云，不需要中心服务器。只需要你的设备。

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Android-blue)

## 功能

- **局域网自动发现** — 设备通过 mDNS 自动发现同一网络内的其他节点，无需手动输入 IP
- **Chunk 存储** — 文件自动切分为 64MB 分块，SHA256 寻址
- **双副本** — 每个 Chunk 存储在 2 个节点上；一个节点离线，文件仍然可读
- **WebDAV** — 在 macOS Finder、Windows 资源管理器、iOS 文件 App、Android 文件管理器中挂载为网络驱动器
- **Web 界面** — 响应式布局，桌面端侧栏导航 + 手机端底部导航，普通/高级模式切换
- **池级认证** — 每个存储池共享一组账号密码，基于 Session 的登录
- **邀请加入** — 池内任意节点审批新节点加入，也支持一次性邀请码
- **本地文件浏览器** — 浏览本机文件并迁移到资源池
- **跨平台** — 每个平台一个静态二进制，无运行时依赖

## 典型使用场景

- **家庭统一存储池** — 复用旧手机、旧平板、旧电脑，把它们组成一个本地存储池，而不是再买 NAS。
- **跨地点随身同步** — 在公司用电脑和手机创建同一个池，上传文件并等待手机完成同步；回家后让家中电脑加入手机所在的同一个池。手机可以把元数据和 Chunk 副本带到另一个网络，家中电脑再从手机同步。
- **WebDAV 访问** — 在 macOS Finder、Windows 资源管理器、iOS 文件 App 或 Android 文件管理器中挂载，像使用局域网盘一样访问。

## 当前 MVP 限制

- PocketCluster 只在当前可达的本地网络内同步；MVP 不提供公网中继、NAT 穿透或永远在线的云端存储。
- 一台设备能否读取某个文件，取决于当前可达节点中是否至少有一个节点持有该文件需要的全部 Chunk。
- 跨地点随身同步的前提是：手机这类随身设备在离开上一个网络前，已经完成所需元数据和 Chunk 副本同步。
- Android 仍是极客模式：后台运行取决于前台服务、电池设置、厂商 ROM 行为，以及设备是否保持在线到足够完成同步。

## 快速开始

### 下载

从 [Releases](#) 下载对应平台的二进制文件：

| 平台 | 文件 |
|------|------|
| macOS (Apple Silicon) | `agent-darwin-arm64` |
| macOS (Intel) | `agent-darwin-amd64` |
| Linux (x86_64) | `agent-linux-amd64` |
| Linux (ARM64) | `agent-linux-arm64` |
| Windows (x86_64) | `agent-windows-amd64.exe` |
| Android | 安装 APK |

### 运行

```bash
# 启动节点
./agent -data ~/pocketcluster -port 7788

# 打开浏览器
open http://localhost:7788
```

1. **创建存储池** — 设置用户名和密码
2. **添加更多设备** — 在另一台设备上启动 agent，打开 Web UI，输入池地址和凭证
3. **审批加入** — 第一台设备上会出现待审批的加入请求，点击 Approve
4. **完成** — 上传到任意节点的文件会自动同步到池内其他节点

### WebDAV

使用任意 WebDAV 客户端连接：

```
http://<ip>:7788/dav/
```

- **macOS Finder** — 前往 → 连接服务器 → `http://<ip>:7788/dav/`
- **Windows 资源管理器** — 映射网络驱动器 → `http://<ip>:7788/dav/`
- **iOS 文件 App** — 连接服务器 → `http://<ip>:7788/dav/`

使用池的用户名和密码认证。

## 架构

```
┌─────────┐      ┌─────────┐      ┌─────────┐
│  Mac    │◄────►│  手机   │◄────►│Windows  │
│ Agent   │      │ Agent   │      │ Agent   │
└────┬────┘      └────┬────┘      └────┬────┘
     │                │                │
     └────────────────┼────────────────┘
                      │
                mDNS 自动发现
                Chunk 同步
                WebDAV /dav/
                Web UI :7788
```

所有节点完全对等 — 没有 Leader，没有 Master，没有 Coordinator。任意节点都可以接收上传、提供下载、审批新成员。

## 从源码构建

```bash
# 前置条件：Go 1.22+，Node.js 18+

# 构建 Web UI
cd web && npm install && npm run build && cd ..

# 构建当前平台
go build -o agent ./cmd/agent

# 交叉编译所有平台
./scripts/build.sh

# 构建 Android APK
cd android && ./gradlew assembleDebug
```

## API

完整 API 文档见 [docs/api-contract.md](docs/api-contract.md)。

## 许可证

MIT
