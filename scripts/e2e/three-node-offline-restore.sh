#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

e2e_init "three-node-offline-restore" 3 "${E2E_BASE_PORT:-17850}"
trap e2e_cleanup EXIT
e2e_start_cluster 3

PAYLOAD_FILE="${E2E_DIR}/recoverable.txt"
PAYLOAD="deleted content must remain recoverable after an offline node returns"
printf '%s\n' "${PAYLOAD}" >"${PAYLOAD_FILE}"

echo "Creating and replicating a directory before deletion..."
curl -fsS -u "${E2E_POOL_USER}:${E2E_POOL_PASS}" -X MKCOL \
  "http://127.0.0.1:${E2E_PORTS[0]}/dav/recoverable" >/dev/null
e2e_upload 0 "/recoverable/payload.txt" "${PAYLOAD_FILE}"
for index in 1 2; do
  e2e_wait_for_download "${index}" "/recoverable/payload.txt" "${PAYLOAD}" || {
    echo "Node $((index + 1)) never received the recoverable file" >&2
    exit 1
  }
done

e2e_stop_node 1
echo "Deleting while node 2 is offline..."
curl -fsS -b "${E2E_COOKIES[0]}" -X DELETE \
  "http://127.0.0.1:${E2E_PORTS[0]}/api/files?path=/recoverable" >/dev/null
e2e_wait_for_not_found 2 "/recoverable/payload.txt" || {
  echo "Online node 3 did not receive the deletion" >&2
  exit 1
}

TRASH_FILE_ID="$(curl -fsS -b "${E2E_COOKIES[0]}" "http://127.0.0.1:${E2E_PORTS[0]}/api/trash" \
  | python3 -c 'import json,sys; entries=json.load(sys.stdin)["data"]["entries"]; print(next(item["file_id"] for item in entries if item["path"] == "/recoverable"))')"
[[ -n "${TRASH_FILE_ID}" ]] || { echo "Deleted directory was not listed in trash" >&2; exit 1; }

echo "Restoring the directory from trash..."
curl -fsS -b "${E2E_COOKIES[0]}" -H "Content-Type: application/json" \
  -d "{\"file_id\":\"${TRASH_FILE_ID}\"}" \
  "http://127.0.0.1:${E2E_PORTS[0]}/api/trash/restore" >/dev/null
e2e_wait_for_download 2 "/recoverable/payload.txt" "${PAYLOAD}" || {
  echo "Online node 3 did not receive the restore" >&2
  exit 1
}

echo "Restarting node 2 and waiting for delete plus restore convergence..."
e2e_start_node 1
e2e_login_node 1
e2e_wait_for_download 1 "/recoverable/payload.txt" "${PAYLOAD}" || {
  echo "Recovered node 2 did not converge to the restored directory" >&2
  exit 1
}

for index in 0 1 2; do
  TRASH_COUNT="$(curl -fsS -b "${E2E_COOKIES[index]}" "http://127.0.0.1:${E2E_PORTS[index]}/api/trash" \
    | python3 -c 'import json,sys; print(sum(item["path"] == "/recoverable" for item in json.load(sys.stdin)["data"]["entries"]))')"
  if [[ "${TRASH_COUNT}" != "0" ]]; then
    echo "Node $((index + 1)) still lists the restored directory in trash" >&2
    exit 1
  fi
done

echo "SUCCESS"
echo "Artifacts retained: ${E2E_DIR}"
