# Roadmap

## Project Direction

PocketCluster is a LAN-first storage pool built from devices the user already owns. The current priority is trustworthy daily use, not expansion into a general cloud-storage product.

Principles:

- Keep the no-leader, no-central-server model.
- Keep the reachable-network scope.
- Prefer visible safety and repairability over new protocol surface.
- Treat Android as an advanced-user node with platform constraints.

## Current v0.8

The storage and recovery core is implemented:

- mDNS discovery on desktop and Android
- authenticated pool join
- chunked storage with two-replica target
- metadata convergence and deterministic conflict handling
- offline delete, rename, trash restore, and version restore
- WebDAV access
- health, repair, integrity, retention, and sync-task views
- file-level replica readiness in the main file view
- verified evacuation of a node before it is retired
- loopback multi-node and WebDAV regression coverage
- connected-device Android smoke helper
- signed Android release workflow and release checksums

The v0.8 release boundary is complete when the full automated suite passes and the connected Android smoke test has been rerun with the release candidate.

## Next: v0.9 Daily Operation

Only pursue these after v0.8 has been used with real files:

- one-command desktop installation and background service setup on Windows and macOS
- clearer recovery actions for blocked node evacuation
- broader Finder, Windows Explorer, and Android WebDAV compatibility evidence
- guided upgrade and rollback instructions
- focused fixes driven by dogfooding evidence

## v1 Decision

Consider v1 after a period of normal use confirms that:

- users can tell when a file is safe without opening diagnostics
- a device can be retired without losing current, trashed, or versioned content
- upgrades preserve pool identity and stored data
- failures become visible and recoverable instead of silent

## Explicitly Not Planned Yet

- public Internet relay or NAT traversal
- multi-user permissions, ACLs, or share links
- SMB
- automatic balancing or node scoring
- erasure coding
- leader-based coordination or a central control plane
