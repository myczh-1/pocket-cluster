# Limitations

PocketCluster is currently a LAN-first storage pool for advanced users. It is not a general cloud drive and not a drop-in NAS replacement.

## Current Scope Limits

- No public Internet relay
- No NAT traversal
- No central always-online coordination service
- No multi-user permissions or ACLs
- No share links

## Availability Limits

- A file is readable only when at least one reachable node currently has every required chunk
- Dual replicas improve fault tolerance but are not the same as a true backup strategy
- Under-replicated files can still be readable, but they are at higher risk until repair completes

## Android Limits

- Android behavior depends on foreground execution, battery policy, and vendor ROM behavior
- A node that appears online is not a guarantee that background sync will keep running indefinitely
- Portable cross-network sync requires the Android device to finish receiving the needed data before leaving the original network

## WebDAV Limits

- WebDAV is intended for local-network clients
- Compatibility has been validated only on a limited client set so far
- Overwrite behavior depends on the client sending the current ETag

## Product Limits

- The current repair and health model is designed to explain state, not to hide all distributed-systems rough edges
- Reliability evidence is still being built through scenario-based testing rather than a broad platform certification matrix
