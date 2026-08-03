#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="${TMPDIR:-/tmp}/pocketcluster-e2e-two-node"
BIN_PATH="${TMP_DIR}/agent"
NODE_A_DIR="${TMP_DIR}/node-a"
NODE_B_DIR="${TMP_DIR}/node-b"
NODE_A_PORT="${NODE_A_PORT:-17788}"
NODE_B_PORT="${NODE_B_PORT:-17789}"
POOL_USER="${POOL_USER:-admin}"
POOL_PASS="${POOL_PASS:-testpass}"
TEST_FILE="${TMP_DIR}/sample.txt"
DELETE_TEST_FILE="${TMP_DIR}/delete-sample.txt"
COOKIE_A="${TMP_DIR}/cookie-a.txt"
COOKIE_B="${TMP_DIR}/cookie-b.txt"
JOIN_OUT="${TMP_DIR}/join-response.json"
NODE_A_LOG="${TMP_DIR}/node-a.log"
NODE_B_LOG="${TMP_DIR}/node-b.log"

cleanup() {
  local exit_code=$?
  if [[ -n "${NODE_A_PID:-}" ]]; then kill "${NODE_A_PID}" >/dev/null 2>&1 || true; fi
  if [[ -n "${NODE_B_PID:-}" ]]; then kill "${NODE_B_PID}" >/dev/null 2>&1 || true; fi
  wait "${NODE_A_PID:-}" >/dev/null 2>&1 || true
  wait "${NODE_B_PID:-}" >/dev/null 2>&1 || true
  if [[ $exit_code -ne 0 ]]; then
    echo "FAILED"
    echo "Root cause: two-node basic E2E did not complete"
    echo "Warnings: inspect ${NODE_A_LOG} and ${NODE_B_LOG}"
    echo "Next action: rerun after checking the pending join and replica health output"
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
require_cmd python3
require_cmd go

mkdir -p "${NODE_A_DIR}" "${NODE_B_DIR}"
rm -rf "${NODE_A_DIR}" "${NODE_B_DIR}"
mkdir -p "${NODE_A_DIR}" "${NODE_B_DIR}"
rm -f "${COOKIE_A}" "${COOKIE_B}" "${JOIN_OUT}" "${NODE_A_LOG}" "${NODE_B_LOG}"

echo "Building agent binary..."
go build -o "${BIN_PATH}" "${ROOT_DIR}/cmd/agent"

echo "Starting node A on port ${NODE_A_PORT}..."
"${BIN_PATH}" -data "${NODE_A_DIR}" -port "${NODE_A_PORT}" -name "node-a" -local-ip 127.0.0.1 -advertise-ip 127.0.0.1 >"${NODE_A_LOG}" 2>&1 &
NODE_A_PID=$!

echo "Starting node B on port ${NODE_B_PORT}..."
"${BIN_PATH}" -data "${NODE_B_DIR}" -port "${NODE_B_PORT}" -name "node-b" -local-ip 127.0.0.1 -advertise-ip 127.0.0.1 >"${NODE_B_LOG}" 2>&1 &
NODE_B_PID=$!

wait_for_health() {
  local port=$1
  for _ in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${port}/api/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_health "${NODE_A_PORT}" || {
  echo "FAILED"
  echo "Root cause: node A health endpoint never became ready"
  echo "Warnings: inspect ${NODE_A_LOG}"
  echo "Next action: verify the agent can bind port ${NODE_A_PORT}"
  exit 1
}

wait_for_health "${NODE_B_PORT}" || {
  echo "FAILED"
  echo "Root cause: node B health endpoint never became ready"
  echo "Warnings: inspect ${NODE_B_LOG}"
  echo "Next action: verify the agent can bind port ${NODE_B_PORT}"
  exit 1
}

echo "Creating pool on node A..."
curl -fsS -c "${COOKIE_A}" -H "Content-Type: application/json" \
  -d "{\"username\":\"${POOL_USER}\",\"password\":\"${POOL_PASS}\"}" \
  "http://127.0.0.1:${NODE_A_PORT}/api/cluster" >/dev/null

NODE_B_ID="$(curl -fsS "http://127.0.0.1:${NODE_B_PORT}/api/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["node_id"])')"

echo "Requesting node B to join node A..."
curl -fsS -o "${JOIN_OUT}" -H "Content-Type: application/json" \
  -d "{\"bootstrap\":\"http://127.0.0.1:${NODE_A_PORT}\",\"pool_user\":\"${POOL_USER}\",\"pool_password\":\"${POOL_PASS}\"}" \
  "http://127.0.0.1:${NODE_B_PORT}/api/join" &
JOIN_PID=$!

for _ in $(seq 1 20); do
  if curl -fsS -b "${COOKIE_A}" "http://127.0.0.1:${NODE_A_PORT}/api/join/pending" | grep -q "${NODE_B_ID}"; then
    break
  fi
  sleep 1
done

echo "Approving node B on node A..."
curl -fsS -b "${COOKIE_A}" -X POST \
  "http://127.0.0.1:${NODE_A_PORT}/api/join/approve/${NODE_B_ID}" >/dev/null

wait "${JOIN_PID}"

echo "Re-authenticating node B after join..."
curl -fsS -c "${COOKIE_B}" -H "Content-Type: application/json" \
  -d "{\"username\":\"${POOL_USER}\",\"password\":\"${POOL_PASS}\"}" \
  "http://127.0.0.1:${NODE_B_PORT}/api/auth/login" >/dev/null

printf 'PocketCluster E2E sample\n' >"${TEST_FILE}"
printf 'PocketCluster retained-delete sample\n' >"${DELETE_TEST_FILE}"

echo "Uploading test file to node A..."
curl -fsS -b "${COOKIE_A}" -F "path=/sample.txt" -F "file=@${TEST_FILE}" \
  "http://127.0.0.1:${NODE_A_PORT}/api/files/upload" >/dev/null

echo "Waiting for node B to see and read the uploaded file..."
READABLE_BEFORE_FAILOVER=0
for _ in $(seq 1 45); do
  if curl -fsS -b "${COOKIE_B}" "http://127.0.0.1:${NODE_B_PORT}/api/files?path=/" | grep -q "/sample.txt"; then
    if DOWNLOADED_NOW="$(curl -fsS -b "${COOKIE_B}" "http://127.0.0.1:${NODE_B_PORT}/api/files/download?path=/sample.txt" 2>/dev/null || true)"; then
      if [[ "${DOWNLOADED_NOW}" == "PocketCluster E2E sample" ]]; then
        READABLE_BEFORE_FAILOVER=1
        break
      fi
    fi
  fi
  curl -fsS -b "${COOKIE_A}" -X POST "http://127.0.0.1:${NODE_A_PORT}/api/jobs/repair-under-replicated" >/dev/null || true
  sleep 1
done

if [[ "${READABLE_BEFORE_FAILOVER}" != "1" ]]; then
  echo "FAILED"
  echo "Root cause: node B never became readable before the failover step"
  echo "Warnings: metadata sync or replica repair did not converge in time"
  echo "Next action: inspect ${NODE_A_LOG}, ${NODE_B_LOG}, and /api/health/summary on both nodes"
  exit 1
fi

echo "Creating and replicating a directory scheduled for deletion..."
curl -fsS -u "${POOL_USER}:${POOL_PASS}" -X MKCOL \
  "http://127.0.0.1:${NODE_A_PORT}/dav/retained-dir" >/dev/null
curl -fsS -b "${COOKIE_A}" -F "path=/retained-dir/delete-sample.txt" -F "file=@${DELETE_TEST_FILE}" \
  "http://127.0.0.1:${NODE_A_PORT}/api/files/upload" >/dev/null

DELETE_FILE_READABLE=0
for _ in $(seq 1 45); do
  if DOWNLOADED_DELETE_FILE="$(curl -fsS -b "${COOKIE_B}" "http://127.0.0.1:${NODE_B_PORT}/api/files/download?path=/retained-dir/delete-sample.txt" 2>/dev/null || true)"; then
    if [[ "${DOWNLOADED_DELETE_FILE}" == "PocketCluster retained-delete sample" ]]; then
      DELETE_FILE_READABLE=1
      break
    fi
  fi
  sleep 1
done

if [[ "${DELETE_FILE_READABLE}" != "1" ]]; then
  echo "FAILED"
  echo "Root cause: node B never received the file used for the deletion propagation check"
  echo "Warnings: metadata sync or replica repair did not converge in time"
  echo "Next action: inspect ${NODE_A_LOG}, ${NODE_B_LOG}, and /api/sync/tasks"
  exit 1
fi

CHUNKS_BEFORE_PURGE="$(find "${NODE_B_DIR}/chunks" -type f | wc -l | tr -d ' ')"

echo "Deleting the replicated directory and waiting for the tombstone to reach node B..."
curl -fsS -b "${COOKIE_A}" -X DELETE \
  "http://127.0.0.1:${NODE_A_PORT}/api/files?path=/retained-dir" >/dev/null

DELETE_PROPAGATED=0
for _ in $(seq 1 45); do
  STATUS_CODE="$(curl -sS -o /dev/null -w '%{http_code}' -b "${COOKIE_B}" "http://127.0.0.1:${NODE_B_PORT}/api/files/download?path=/retained-dir/delete-sample.txt" || true)"
  if [[ "${STATUS_CODE}" == "404" ]]; then
    DELETE_PROPAGATED=1
    break
  fi
  sleep 1
done

if [[ "${DELETE_PROPAGATED}" != "1" ]]; then
  echo "FAILED"
  echo "Root cause: deleted directory tombstone did not reach node B"
  echo "Warnings: node B can still download a file under the deleted directory"
  echo "Next action: inspect ${NODE_A_LOG}, ${NODE_B_LOG}, and event sync tasks"
  exit 1
fi

echo "Immediately purging the deleted directory..."
PURGE_JOB_ID="$(curl -fsS -b "${COOKIE_A}" -X POST "http://127.0.0.1:${NODE_A_PORT}/api/jobs/purge-retained-data" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["id"])')"
PURGE_DONE=0
for _ in $(seq 1 45); do
  PURGE_STATUS="$(curl -fsS -b "${COOKIE_A}" "http://127.0.0.1:${NODE_A_PORT}/api/jobs/${PURGE_JOB_ID}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["status"])')"
  if [[ "${PURGE_STATUS}" == "done" ]]; then
    PURGE_DONE=1
    break
  fi
  if [[ "${PURGE_STATUS}" == "failed" || "${PURGE_STATUS}" == "blocked" ]]; then
    break
  fi
  sleep 1
done

if [[ "${PURGE_DONE}" != "1" ]]; then
  echo "FAILED"
  echo "Root cause: immediate purge job did not finish successfully (${PURGE_STATUS})"
  echo "Warnings: inspect ${NODE_A_LOG} and the purge job ${PURGE_JOB_ID}"
  echo "Next action: inspect /api/jobs/${PURGE_JOB_ID} and /api/sync/tasks on node A"
  exit 1
fi

PURGE_PROPAGATED=0
for _ in $(seq 1 45); do
  CHUNKS_AFTER_PURGE="$(find "${NODE_B_DIR}/chunks" -type f | wc -l | tr -d ' ')"
  if [[ "${CHUNKS_AFTER_PURGE}" -lt "${CHUNKS_BEFORE_PURGE}" ]]; then
    PURGE_PROPAGATED=1
    break
  fi
  sleep 1
done

if [[ "${PURGE_PROPAGATED}" != "1" ]]; then
  echo "FAILED"
  echo "Root cause: node B kept the unreferenced chunk after directory purge propagated"
  echo "Warnings: chunk count stayed at ${CHUNKS_BEFORE_PURGE}"
  echo "Next action: inspect ${NODE_B_LOG} and the FILE_PURGE / DIR_PURGE events"
  exit 1
fi

echo "Stopping node A to verify read-after-node-loss..."
kill "${NODE_A_PID}" >/dev/null 2>&1 || true
wait "${NODE_A_PID}" >/dev/null 2>&1 || true
unset NODE_A_PID
sleep 2

DOWNLOADED="$(curl -fsS -b "${COOKIE_B}" "http://127.0.0.1:${NODE_B_PORT}/api/files/download?path=/sample.txt")"
if [[ "${DOWNLOADED}" != "PocketCluster E2E sample" ]]; then
  echo "FAILED"
  echo "Root cause: node B could not read the replicated file after node A stopped"
  echo "Warnings: downloaded content did not match expected sample"
  echo "Next action: inspect ${NODE_B_LOG} and /api/health/summary before stopping node A"
  exit 1
fi

echo "SUCCESS"
echo "Root cause: none"
echo "Warnings: node B still needs a valid WebUI session for authenticated checks"
echo "Next action: extend this script with larger files or mid-upload interruption scenarios"
