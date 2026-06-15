# PocketCluster

[中文说明](README_zh.md)

> **Early MVP** — PocketCluster is in active development. Many features are rough and the API may change. Not recommended for production use yet.

Turn your old phones, laptops, and tablets into a unified storage pool.

No NAS. No cloud. No central server. Just your devices.

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Android-blue)

## Features

- **LAN Discovery** — devices find each other automatically via mDNS, no manual IP entry
- **Chunk Storage** — files split into 64MB chunks, addressed by SHA256 hash
- **Dual Replicas** — every chunk stored on 2 nodes; one goes offline, file still readable
- **WebDAV** — mount as a network drive in macOS Finder, Windows Explorer, iOS Files, Android file managers
- **Web UI** — responsive desktop sidebar + mobile bottom nav, Normal/Advanced mode
- **Pool Auth** — shared username/password per storage pool, session-based login
- **Invite Join** — approve new nodes from any existing member, or use one-time invite tokens
- **Local File Browser** — browse local files and migrate them into the pool
- **Cross-Platform** — single static binary for each platform, no runtime dependencies

## Example Use Cases

- **Home storage pool** — reuse old phones, tablets, laptops, and desktops as one local storage pool instead of buying a NAS.
- **Portable sync between places** — create a pool with your laptop and phone at work, upload files while both are online, then bring the phone home and let a home computer join the same pool. The phone can carry metadata and chunk replicas between networks, so the home computer can sync from it.
- **WebDAV access** — mount the pool from Finder, Windows Explorer, iOS Files, or Android file managers and use it like a local network drive.

## Current MVP Limits

- PocketCluster syncs over reachable local networks. It does not provide public Internet relay, NAT traversal, or always-online cloud storage.
- A file is readable on a device only when at least one currently reachable node has every required chunk.
- Portable sync works only after the carrying device, such as a phone, has finished receiving the needed metadata and chunk replicas before leaving the previous network.
- Android is still geek-mode: background execution depends on foreground service, battery settings, vendor ROM behavior, and the device staying online long enough to sync.

## Quick Start

### Download

Grab the latest binary for your platform from [Releases](#).

| Platform | Binary |
|----------|--------|
| macOS (Apple Silicon) | `agent-darwin-arm64` |
| macOS (Intel) | `agent-darwin-amd64` |
| Linux (x86_64) | `agent-linux-amd64` |
| Linux (ARM64) | `agent-linux-arm64` |
| Windows (x86_64) | `agent-windows-amd64.exe` |
| Android | Install the APK |

### Run

```bash
# Start the agent
./agent -data ~/pocketcluster -port 7788

# Open in browser
open http://localhost:7788
```

1. **Create a pool** — set a username and password
2. **Add more devices** — run the agent on another machine, open the Web UI, enter the pool address + credentials
3. **Approve** — the first device shows a pending join request, click Approve
4. **Done** — files uploaded to any node are replicated across the pool

### WebDAV

Connect from any WebDAV client:

```
http://<ip>:7788/dav/
```

- **macOS Finder** — Go → Connect to Server → `http://<ip>:7788/dav/`
- **Windows Explorer** — Map network drive → `http://<ip>:7788/dav/`
- **iOS Files** — Connect to Server → `http://<ip>:7788/dav/`

Authenticate with your pool username and password.

Existing-file overwrites are conditional: clients must send the current ETag in `If-Match`; blind overwrites are rejected with `428 Precondition Required`.

## Architecture

```
┌─────────┐      ┌─────────┐      ┌─────────┐
│  Mac    │◄────►│  Phone  │◄────►│ Windows │
│ Agent   │      │ Agent   │      │ Agent   │
└────┬────┘      └────┬────┘      └────┬────┘
     │                │                │
     └────────────────┼────────────────┘
                      │
                mDNS Discovery
                Chunk Sync
                WebDAV /dav/
                Web UI :7788
```

Each node is equal — no leader, no master, no coordinator. Any node can accept uploads, serve downloads, and approve new members.

## Build from Source

```bash
# Prerequisites: Go 1.22+, Node.js 18+

# Build web UI
cd web && npm install && npm run build && cd ..

# Build for current platform
go build -o agent ./cmd/agent

# Cross-compile all platforms
./scripts/build.sh

# Build Android APK
cd android && ./gradlew assembleDebug
```

## API

See [docs/api-contract.md](docs/api-contract.md) for the full API reference.

## License

MIT
