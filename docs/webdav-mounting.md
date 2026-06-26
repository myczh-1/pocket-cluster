# WebDAV Mounting Guide

PocketCluster exposes the storage pool through WebDAV at:

```text
http://<agent-address>:7788/dav/
```

Use the same pool username and password that you use for the WebUI. WebDAV writes are normal pool uploads: they copy data into PocketCluster and do not delete or move the source file on your computer.

## macOS Finder

1. Open Finder.
2. Choose Go > Connect to Server.
3. Enter `http://<agent-address>:7788/dav/`.
4. Sign in with the pool username and password.

## Windows

1. Open This PC.
2. Choose Add a network location.
3. Enter `http://<agent-address>:7788/dav/`.
4. Sign in with the pool username and password when prompted.

Windows WebDAV clients may cache credentials aggressively. If a password change is not picked up, remove the saved credential from Windows Credential Manager and reconnect.

## Android

Use a file manager that supports WebDAV. Add a WebDAV server with:

```text
URL: http://<agent-address>:7788/dav/
Username: <pool username>
Password: <pool password>
```

Keep the Android device on the same LAN as the PocketCluster agent unless a future release explicitly adds remote access.

## curl Smoke Tests

Upload a file:

```bash
curl -u <pool-user>:<pool-password> -T ./photo.jpg http://<agent-address>:7788/dav/photo.jpg
```

Download a file:

```bash
curl -u <pool-user>:<pool-password> -o ./photo.jpg http://<agent-address>:7788/dav/photo.jpg
```

List the root directory:

```bash
curl -u <pool-user>:<pool-password> -X PROPFIND -H "Depth: 1" http://<agent-address>:7788/dav/
```

## Compatibility Notes

- Large uploads are streamed into PocketCluster chunks instead of being buffered as a single in-memory file.
- Overwriting an existing file requires the WebDAV client to send the current ETag. This protects against blind overwrites from stale clients.
- Directory creation, rename, delete, upload, download, and Basic Auth are covered by backend tests.
- SMB is intentionally not part of this v2 slice; WebDAV is the low-complexity standard mount path.
