# Reliability Test Report

This document records scenario-based validation for PocketCluster `v0.2.x`.

It is not a marketing artifact. Its purpose is to answer:

- what was tested
- what actually passed
- where the system still has rough edges

## Test Environment

Fill this section for each run:

- Date: 2026-06-29
- Commit: local working tree after `b65fb45` plus `v0.2.4` reliability scripts/docs changes
- OS / hardware: macOS local development machine
- Agent binary source: `go build ./cmd/agent`
- Pool topology: loopback-hosted one-node and two-node local agents
- Network assumptions: local-only validation on `127.0.0.1`

## Scenario Matrix

| Scenario | Goal | Method | Result | Notes |
|---|---|---|---|---|
| Two-node basic | Verify join, replicate, and read-after-node-loss | `scripts/e2e/two-node-basic.sh` | Passed locally | loopback-only validation |
| WebDAV smoke | Verify upload, list, download, delete on one node | `scripts/e2e/webdav-smoke-test.sh` | Passed locally | single-node local validation |
| Android manual | Verify Android join and carry behavior | `scripts/e2e/android-manual-test.md` | Pending |  |

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

### Android manual

- Status:
- Evidence:
- Failure mode if any:

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
