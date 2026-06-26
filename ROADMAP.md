# Roadmap

## Project Direction

PocketCluster is currently optimizing for one thing: a trustworthy local storage pool built from existing devices on reachable local networks.

The next phase is not about expanding into a broader cloud-storage product. It is about making the current pool understandable and safe enough that an advanced user can trust it with real files.

Current principles:

- Keep the no-leader, no-central-server model
- Keep LAN-first scope
- Prefer observability and repairability over new feature surface
- Treat Android as a constrained advanced-user target, not as a background-reliable mobile product

## v0.1.x - MVP Stabilization

Goal: freeze the current MVP boundary and describe it accurately.

Scope:

- Align `README.md`, `README_zh.md`, `docs/api-contract.md`, `docs/product-spec.md`, and `docs/feature-list.md`
- Clarify what is supported, experimental, and explicitly not supported
- Make WebDAV, Android, dual replicas, and health visibility status consistent across docs
- Cut `v0.1.0` only after the documentation matches actual behavior

Exit criteria:

- No major contradictions between README, product spec, feature list, and API contract
- A new user can tell whether the current version fits their use case

## v0.2.0 - Trustworthy Local Storage Pool

Goal: answer two user questions clearly:

- Are my files safe right now?
- If not, what is the system doing about it?

### v0.2.1 Documentation and State Model

Goal: define one shared language before building more UI or operations.

Canonical health states:

- `healthy`
- `under_replicated`
- `unavailable`
- `repairing`

Canonical sync task states:

- `pending`
- `running`
- `retrying`
- `blocked`
- `failed`
- `done`

Notes:

- `orphan` should be treated as a diagnostic finding, not a top-level health state
- `missing` should be represented through `unavailable` unless a lower-level diagnostic report needs more detail

### v0.2.2 Observability

Goal: expose the current state of the pool without forcing users to infer it from raw logs.

Primary surfaces:

- `Health` page
- `Sync Tasks` page

Health should answer:

- Is a file currently readable?
- Is it fully replicated?
- Which files are at risk?
- Which nodes are contributing to that risk?

Sync Tasks should answer:

- What is currently syncing?
- What is currently repairing?
- What is waiting to retry?
- What is blocked and why?

### v0.2.3 Repair and Operations

Goal: unify manual and automatic repair entry points behind one backend action model.

Initial job types:

- `rescan`
- `repair_under_replicated`
- `integrity_check`

Target API shape:

- `POST /api/jobs/rescan`
- `POST /api/jobs/repair-under-replicated`
- `POST /api/jobs/integrity-check`
- `GET /api/jobs`
- `GET /api/jobs/{jobId}`

The same backend model should be reusable from:

- Web UI
- CLI
- background scheduling

### v0.2.4 Reliability Validation

Goal: prove trustworthy behavior with scenario-based testing instead of only feature completeness.

Minimum validation areas:

- two-node upload and read-after-node-loss
- three-node replica recovery
- WebDAV smoke coverage
- Android manual validation

Expected artifacts:

- `scripts/e2e/`
- `docs/reliability-test-report.md`
- `docs/troubleshooting.md`
- `docs/limitations.md`

## v0.3.0 - Usability

Reserved for later. Likely focus:

- smoother onboarding
- better Android guidance
- better mount/setup guidance
- simpler diagnostics for non-authors

## v0.4.0 - Project Presentation

Reserved for later. Likely focus:

- screenshots
- short demo video
- architecture diagram
- polished reliability evidence

## Explicitly Not Planned Yet

The following are intentionally outside the current roadmap:

- public Internet relay
- NAT traversal
- multi-user permissions
- ACLs
- share links
- automatic balancing
- erasure coding
- Raft or leader-based coordination
- central metadata index or control plane
