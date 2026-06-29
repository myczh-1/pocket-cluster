# Android Manual Test

This checklist is for validating the current advanced-user Android flow in a real local-network pool.

## Environment

- 1 desktop or laptop node already hosting a PocketCluster pool
- 1 Android device on the same LAN
- PocketCluster Android build installed
- WebUI reachable from the Android device

## Scenario 1: Join And Basic Connectivity

1. Start the desktop node and create a pool.
2. Start the Android agent.
3. Join the Android device to the existing pool with pool credentials or an invite token.
4. Approve the pending join from an existing trusted node if required.
5. Confirm the Android node appears in `Nodes`.

Expected:

- Android node shows `online`
- Android node reports storage capacity
- Join finishes without manual database cleanup

## Scenario 2: File Upload And Readback

1. Upload a small file from the desktop node.
2. Wait for `Health` to report healthy or under-replicated but readable status.
3. Open the file from Android through the WebUI or a WebDAV-capable file manager.

Expected:

- File is visible on Android
- File contents match the source
- No silent corruption or duplicate conflict file appears

## Scenario 3: Carry Metadata And Replicas Between Networks

1. Keep the Android device online until `Health` shows the file is readable and replicated.
2. Disconnect the Android device from the original network.
3. Bring it to a second local network with another PocketCluster desktop node.
4. Join or reconnect the second desktop node to the same pool.
5. Wait for sync to continue from the Android device.

Expected:

- Android can still advertise the pool state on the second network
- The second desktop node can fetch metadata and file content from Android
- `Sync Tasks` shows ongoing pull and repair activity rather than silent stalls

## Scenario 4: Background Reliability

1. Start a medium-size upload while Android is online.
2. Lock the Android device.
3. Leave it idle for several minutes.
4. Reopen the app and inspect `Sync Tasks` and `Health`.

Expected:

- The system either continues syncing or clearly reports stalled / retrying / blocked work
- No false `healthy` result is shown if the file is incomplete

## Failure Notes To Capture

Record:

- device model and Android version
- vendor ROM
- whether battery optimization is enabled
- whether the device was on charger
- exact failure symptom
- relevant `Sync Tasks` rows
- relevant `Health` state
- relevant agent log lines
