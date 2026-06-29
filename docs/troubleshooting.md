# Troubleshooting

## Agent Does Not Start

Symptoms:

- `curl http://127.0.0.1:<port>/api/health` fails
- browser cannot reach the WebUI

Checks:

- confirm the port is not already occupied
- inspect the agent log output
- run `./agent doctor -port <port> -data <data-dir>`

## Join Request Never Finishes

Symptoms:

- `Sync Tasks` shows metadata pull or push retrying
- joining node waits for approval and never finishes

Checks:

- confirm the joining node appears under `Pending join requests`
- approve the join from an existing trusted node
- confirm both nodes can reach each other on the local network
- check whether the joining node advertised a loopback address accidentally

## File Is Visible But Not Readable

Symptoms:

- file appears in `Files`
- download fails or `Health` shows `unavailable`

Checks:

- inspect `Health` for the file and the referenced chunks
- confirm at least one currently reachable node has every required chunk
- inspect `Sync Tasks` for blocked or retrying replica repair work

## Replica Coverage Stays Under-Replicated

Symptoms:

- `Health` remains `under_replicated`
- repair jobs finish as `retrying` or `blocked`

Checks:

- verify another trusted node is online and has enough free space
- run `Repair under-replicated` from the `Sync Tasks` page
- rerun a health `rescan`
- inspect agent logs for chunk fetch or storage errors

## WebDAV Connects But File Operations Fail

Symptoms:

- mount succeeds
- upload, overwrite, or delete fails

Checks:

- confirm you are using the pool username and password
- rerun `scripts/e2e/webdav-smoke-test.sh`
- for overwrite failures, check whether the client supports ETag-based conditional writes
- inspect the agent log for WebDAV-specific errors

## Android Looks Online But Sync Does Not Progress

Symptoms:

- Android node shows in `Nodes`
- `Sync Tasks` stalls or keeps retrying

Checks:

- confirm the Android device is on the same LAN
- disable aggressive battery optimization for the app if possible
- keep the app foregrounded during validation
- capture the exact `Sync Tasks` row and `Health` status before retrying
