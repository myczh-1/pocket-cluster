#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="${TMPDIR:-/tmp}/pocketcluster-e2e-webdav"
BIN_PATH="${TMP_DIR}/agent"
DATA_DIR="${TMP_DIR}/node"
PORT="${PORT:-17790}"
POOL_USER="${POOL_USER:-admin}"
POOL_PASS="${POOL_PASS:-testpass}"
LOG_FILE="${TMP_DIR}/agent.log"
UPLOAD_FILE="${TMP_DIR}/webdav-upload.txt"
DOWNLOAD_FILE="${TMP_DIR}/webdav-download.txt"

cleanup() {
  local exit_code=$?
  if [[ -n "${AGENT_PID:-}" ]]; then kill "${AGENT_PID}" >/dev/null 2>&1 || true; fi
  wait "${AGENT_PID:-}" >/dev/null 2>&1 || true
  if [[ $exit_code -ne 0 ]]; then
    echo "FAILED"
    echo "Root cause: WebDAV smoke test did not complete"
    echo "Warnings: inspect ${LOG_FILE}"
    echo "Next action: rerun after checking WebDAV auth and file write logs"
  fi
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "FAILED"
    echo "Root cause: required command '$1' is missing"
    echo "Warnings: install '$1' and rerun"
    echo "Next action: install the missing dependency"
    exit 1
  }
}

require_cmd curl
require_cmd go

mkdir -p "${DATA_DIR}"
rm -f "${LOG_FILE}" "${UPLOAD_FILE}" "${DOWNLOAD_FILE}"

echo "Building agent binary..."
go build -o "${BIN_PATH}" "${ROOT_DIR}/cmd/agent"

echo "Starting agent on port ${PORT}..."
"${BIN_PATH}" -data "${DATA_DIR}" -port "${PORT}" -name "webdav-smoke" -local-ip 127.0.0.1 >"${LOG_FILE}" 2>&1 &
AGENT_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

curl -fsS -H "Content-Type: application/json" \
  -d "{\"username\":\"${POOL_USER}\",\"password\":\"${POOL_PASS}\"}" \
  "http://127.0.0.1:${PORT}/api/cluster" >/dev/null

printf 'webdav smoke payload\n' >"${UPLOAD_FILE}"

echo "Uploading through WebDAV..."
curl -fsS -u "${POOL_USER}:${POOL_PASS}" -T "${UPLOAD_FILE}" \
  "http://127.0.0.1:${PORT}/dav/webdav-upload.txt" >/dev/null

echo "Listing root via PROPFIND..."
curl -fsS -u "${POOL_USER}:${POOL_PASS}" -X PROPFIND -H "Depth: 1" \
  "http://127.0.0.1:${PORT}/dav/" >/dev/null

echo "Downloading through WebDAV..."
curl -fsS -u "${POOL_USER}:${POOL_PASS}" -o "${DOWNLOAD_FILE}" \
  "http://127.0.0.1:${PORT}/dav/webdav-upload.txt"

if ! cmp -s "${UPLOAD_FILE}" "${DOWNLOAD_FILE}"; then
  echo "FAILED"
  echo "Root cause: downloaded WebDAV content did not match the uploaded file"
  echo "Warnings: compare ${UPLOAD_FILE} and ${DOWNLOAD_FILE}"
  echo "Next action: inspect ${LOG_FILE} and /api/health/summary"
  exit 1
fi

echo "Deleting through WebDAV..."
curl -fsS -u "${POOL_USER}:${POOL_PASS}" -X DELETE \
  "http://127.0.0.1:${PORT}/dav/webdav-upload.txt" >/dev/null

echo "SUCCESS"
echo "Root cause: none"
echo "Warnings: this script validates a single local agent only"
echo "Next action: extend with overwrite and larger-file scenarios if needed"
