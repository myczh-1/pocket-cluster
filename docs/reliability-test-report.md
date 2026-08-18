# Reliability Test Report

This document records scenario-based validation for PocketCluster `v0.8.x`.

It is not a marketing artifact. Its purpose is to answer:

- what was tested
- what actually passed
- where the system still has rough edges

## Test Environment

Fill this section for each run:

- Date: 2026-08-18
- Commit: `61a0b4e` plus the v0.8 readiness and safe-exit work
- OS / hardware: macOS development machine and Xiaomi Android 16 phone
- Agent binary source: `go build ./cmd/agent`
- Pool topology: loopback-hosted multi-node agents plus a real Wi-Fi desktop/Android pool
- Network assumptions: loopback automation and same-LAN Wi-Fi validation

## Scenario Matrix

| Scenario | Goal | Method | Result | Notes |
|---|---|---|---|---|
| Two-node basic | Verify join, replicate, and read-after-node-loss | `scripts/e2e/two-node-basic.sh` | Passed locally | loopback-only validation |
| Three-node offline deletion | Verify delete, immediate purge, and recovered-node chunk cleanup | `scripts/e2e/three-node-offline-delete.sh` | Passed locally | loopback-only validation |
| Three-node offline rename | Verify rename convergence and no stale path after a node recovers | `scripts/e2e/three-node-offline-rename.sh` | Passed locally | loopback-only validation |
| Three-node concurrent update | Verify two offline WebDAV edits converge regardless of recovery order | `scripts/e2e/three-node-offline-concurrent-update.sh` | Passed locally | main plus conflict, restart, continued edit |
| Three-node offline restore | Verify trash restore converges after an offline node recovers | `scripts/e2e/three-node-offline-restore.sh` | Passed locally | directory tree and retained chunks |
| Three-node version restore | Verify superseded content restore converges after an offline node recovers | `scripts/e2e/three-node-offline-version-restore.sh` | Passed locally | bounded history retention and lineage |
| WebDAV smoke | Verify upload, list, download, delete on one node | `scripts/e2e/webdav-smoke-test.sh` | Passed locally | single-node local validation |
| Android manual | Verify Android join, background survival, native restart, discovery, and bidirectional replication | real device plus `scripts/e2e/android-manual-test.md` | Passed | Xiaomi Android 16 on Wi-Fi |
| Android connected smoke | Verify install/start and agent health over ADB | `scripts/e2e/android-connected-smoke.sh` | Available | connected-device regression helper |
| Node safe exit | Verify one node's chunk is copied and verified on two other nodes | `go test ./internal/server` | Passed locally | three real HTTP agent instances in test |

## Latest Run Summary

### Two-node basic

- Status: Passed locally
- Evidence:
  - node B joined node A through the documented pending-join approval flow
  - file upload to node A became readable from node B
  - node A was stopped and node B still downloaded the expected file contents
- Failure mode if any:
  - first draft of the script failed because it stopped node A before node B had actually converged; the script now waits for node B readability before failover

### WebDAV smoke

- Status: Passed locally
- Evidence:
  - WebDAV upload succeeded
  - root directory `PROPFIND` succeeded
  - download matched uploaded content
  - delete succeeded
- Failure mode if any:
  - none in the local single-node scenario

### Three-node offline deletion

- Status: Passed locally
- Evidence:
  - node 2 received a replica, then was stopped before deletion
  - node 1 deleted the containing directory and completed an immediate purge while node 3 remained online
  - node 3 converged to the deletion while node 2 was offline
  - after node 2 restarted, the deleted file stayed unavailable and its local unreferenced Chunk was removed
- Failure mode if any:
  - none in the loopback scenario

### Three-node offline rename

- Status: Passed locally
- Evidence:
  - node 2 received the original file, then was stopped before the rename
  - node 3 converged to the new path while node 2 remained offline
  - after node 2 restarted, it read the new path, rejected the old path, and listed exactly one live entry
- Failure mode if any:
  - none in the loopback scenario

### Three-node concurrent update

- Status: Passed locally
- Evidence:
  - two isolated nodes conditionally overwrote the same base ETag
  - both recovery orders converged to identical normalized metadata
  - one deterministic main file and one conflict copy preserved both contents
  - a full-cluster restart kept the same projection
  - a later update on the winning branch stayed on the main path
- Failure mode if any:
  - none in the loopback scenario

### Three-node offline restore

- Status: Passed locally
- Evidence:
  - node 2 received a replicated directory and file, then was stopped
  - node 1 deleted the directory and restored it from the trash while node 2 was offline
  - node 3 applied both events while remaining online
  - after node 2 restarted, the full directory tree and original content were readable again
  - the trash entry disappeared on all three nodes
- Failure mode if any:
  - none in the loopback scenario

### Three-node version restore

- Status: Passed locally
- Evidence:
  - the first file version replicated to all three nodes before replacement
  - the second version became current on all nodes while the first version's Chunks remained recoverable
  - node 2 was stopped before node 1 restored the first content as a new version
  - node 3 converged while online; node 2 converged after restart
  - all three nodes agreed on the same restored current version ID and content
  - unit coverage verifies expired historical Chunks are reclaimed after the recovery window
- Failure mode if any:
  - none in the loopback scenario

### Android manual

- Status: Passed on one Xiaomi Android 16 device
- Evidence:
  - upgrade install preserved the node and pool state
  - the foreground agent survived screen-off/background use and recovered from a native-process restart
  - Android and desktop discovered each other through mDNS on real Wi-Fi
  - file replication worked in both directions and remained readable after the source node stopped
- Failure mode if any:
  - the initial Android mDNS path did not work; the Android NSD bridge now performs native discovery and registration

## Known Reliability Gaps

Track concrete gaps only. Examples:

- Android background execution remains vendor- and battery-policy-dependent
- Some repair flows still require more than one sync pass before status stabilizes
- WebDAV client compatibility has not yet been validated across a broad matrix

## Exit Criteria For “Trustworthy Local Storage Pool”

Treat the current phase as complete only when:

- two-node basic automation passes consistently
- WebDAV smoke passes consistently
- Android manual validation has at least one successful full run
- `Health` and `Sync Tasks` explain failures instead of leaving silent ambiguity
