#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

e2e_init "three-node-offline-delete" 3 "${E2E_BASE_PORT:-17810}"
trap e2e_cleanup EXIT
e2e_start_cluster 3

PAYLOAD_FILE="${E2E_DIR}/offline-delete.txt"
PAYLOAD="offline deletion must converge after node recovery"
printf '%s\n' "${PAYLOAD}" >"${PAYLOAD_FILE}"

echo "Creating a directory and uploading its file through node 2..."
curl -fsS -u "${E2E_POOL_USER}:${E2E_POOL_PASS}" -X MKCOL \
  "http://127.0.0.1:${E2E_PORTS[0]}/dav/offline-delete" >/dev/null
e2e_upload 1 "/offline-delete/payload.txt" "${PAYLOAD_FILE}"
e2e_wait_for_download 0 "/offline-delete/payload.txt" "${PAYLOAD}" || {
  echo "Node 1 never received the file metadata before node 2 went offline" >&2
  exit 1
}

CHUNKS_BEFORE="$(e2e_chunk_count 1)"
if [[ "${CHUNKS_BEFORE}" -eq 0 ]]; then
  echo "Node 2 did not retain its locally uploaded chunk before going offline" >&2
  exit 1
fi
e2e_stop_node 1

echo "Deleting and purging while node 2 is offline..."
curl -fsS -b "${E2E_COOKIES[0]}" -X DELETE \
  "http://127.0.0.1:${E2E_PORTS[0]}/api/files?path=/offline-delete" >/dev/null

RETAINED_DATA_IDLE=0
for _ in $(seq 1 45); do
  REPAIR_STATE="$(curl -fsS -b "${E2E_COOKIES[0]}" "http://127.0.0.1:${E2E_PORTS[0]}/api/health/insights" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("{}:{}".format(d["repair"]["status"], d["repair"]["queued_chunks"]))')"
  if [[ "${REPAIR_STATE}" == "idle:0" ]]; then
    RETAINED_DATA_IDLE=1
    break
  fi
  sleep 1
done
if [[ "${RETAINED_DATA_IDLE}" != "1" ]]; then
  echo "Retained deleted data was incorrectly queued for replica repair (${REPAIR_STATE})" >&2
  exit 1
fi

PURGE_JOB_ID="$(e2e_start_purge 0)"
e2e_wait_for_job 0 "${PURGE_JOB_ID}" || {
  echo "Immediate purge did not finish on node 1" >&2
  exit 1
}
e2e_wait_for_not_found 2 "/offline-delete/payload.txt" || {
  echo "Online node 3 did not receive the deletion" >&2
  exit 1
}

echo "Restarting node 2 and waiting for deletion convergence..."
e2e_start_node 1
curl -fsS -c "${E2E_COOKIES[1]}" -H "Content-Type: application/json" \
  -d "{\"username\":\"${E2E_POOL_USER}\",\"password\":\"${E2E_POOL_PASS}\"}" \
  "http://127.0.0.1:${E2E_PORTS[1]}/api/auth/login" >/dev/null
e2e_wait_for_not_found 1 "/offline-delete/payload.txt" || {
  echo "Recovered node 2 still exposes the deleted file" >&2
  exit 1
}

for _ in $(seq 1 45); do
  if [[ "$(e2e_chunk_count 1)" -lt "${CHUNKS_BEFORE}" ]]; then
    echo "SUCCESS"
    echo "Artifacts retained: ${E2E_DIR}"
    exit 0
  fi
  sleep 1
done

echo "Recovered node 2 kept its unreferenced chunk after receiving purge events" >&2
exit 1
