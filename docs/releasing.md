# Releasing PocketCluster

Tagged releases build desktop binaries, a signed Android APK, and `SHA256SUMS`.

Configure these GitHub Actions secrets before pushing a `v*` tag:

- `ANDROID_KEYSTORE_BASE64`: base64-encoded release keystore
- `ANDROID_KEYSTORE_PASSWORD`: keystore password
- `ANDROID_KEY_ALIAS`: signing key alias
- `ANDROID_KEY_PASSWORD`: signing key password

The release workflow intentionally fails instead of publishing an unsigned or debug APK when any signing secret is missing.

Before tagging:

1. Keep `web/package.json` and Android `versionName` aligned with the tag.
2. Increment Android `versionCode`.
3. Run `go test ./...`, the Web build, the loopback E2E suite, and the connected Android smoke test when a device is available.
4. Push the tag and verify the APK signature and `SHA256SUMS` from the generated release.
