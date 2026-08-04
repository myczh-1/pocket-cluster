#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

e2e_init "three-node-offline-rename" 3 "${E2E_BASE_PORT:-17820}"
trap e2e_cleanup EXIT
e2e_start_cluster 3

PAYLOAD_FILE="${E2E_DIR}/offline-rename.txt"
PAYLOAD="offline rename must converge without duplicate entries"
printf '%s\n' "${PAYLOAD}" >"${PAYLOAD_FILE}"

echo "Replicating a file to node 2 before its temporary outage..."
e2e_upload 0 "/before-rename.txt" "${PAYLOAD_FILE}"
e2e_wait_for_download 1 "/before-rename.txt" "${PAYLOAD}" || {
  echo "Node 2 never received the file before going offline" >&2
  exit 1
}

e2e_stop_node 1

echo "Renaming while node 2 is offline..."
e2e_rename 0 "/before-rename.txt" "/after-rename.txt"
e2e_wait_for_download 2 "/after-rename.txt" "${PAYLOAD}" || {
  echo "Online node 3 did not receive the renamed path" >&2
  exit 1
}
e2e_wait_for_not_found 2 "/before-rename.txt" || {
  echo "Online node 3 still exposes the old path" >&2
  exit 1
}

echo "Restarting node 2 and checking recovered metadata..."
e2e_start_node 1
e2e_login_node 1
e2e_wait_for_download 1 "/after-rename.txt" "${PAYLOAD}" || {
  echo "Recovered node 2 did not receive the renamed path" >&2
  exit 1
}
e2e_wait_for_not_found 1 "/before-rename.txt" || {
  echo "Recovered node 2 still exposes the old path" >&2
  exit 1
}

ENTRY_COUNTS="$(curl -fsS -b "${E2E_COOKIES[1]}" "http://127.0.0.1:${E2E_PORTS[1]}/api/files?path=/" \
  | python3 -c 'import json,sys; entries=json.load(sys.stdin)["data"]["entries"]; print("{}:{}".format(sum(e["path"] == "/after-rename.txt" for e in entries), sum(e["path"] == "/before-rename.txt" for e in entries)))')"
if [[ "${ENTRY_COUNTS}" != "1:0" ]]; then
  echo "Recovered node 2 has duplicate or stale rename entries (${ENTRY_COUNTS})" >&2
  exit 1
fi

echo "SUCCESS"
echo "Artifacts retained: ${E2E_DIR}"
