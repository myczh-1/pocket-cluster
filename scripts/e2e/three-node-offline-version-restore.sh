#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

e2e_init "three-node-offline-version-restore" 3 "${E2E_BASE_PORT:-17870}"
trap e2e_cleanup EXIT
e2e_start_cluster 3

FIRST_FILE="${E2E_DIR}/first.txt"
SECOND_FILE="${E2E_DIR}/second.txt"
FIRST_CONTENT="first recoverable version"
SECOND_CONTENT="second current version"
printf '%s' "${FIRST_CONTENT}" >"${FIRST_FILE}"
printf '%s' "${SECOND_CONTENT}" >"${SECOND_FILE}"

echo "Creating and replicating the first version..."
e2e_upload 0 "/history.txt" "${FIRST_FILE}"
for index in 1 2; do
  e2e_wait_for_download "${index}" "/history.txt" "${FIRST_CONTENT}" || {
    echo "Node $((index + 1)) never received the first version" >&2
    exit 1
  }
done

FILE_RECORD="$(curl -fsS -b "${E2E_COOKIES[0]}" "http://127.0.0.1:${E2E_PORTS[0]}/api/files?path=/" \
  | python3 -c 'import json,sys; item=next(x for x in json.load(sys.stdin)["data"]["entries"] if x["path"] == "/history.txt"); print(item["file_id"], item["version_id"])')"
read -r FILE_ID FIRST_VERSION_ID <<<"${FILE_RECORD}"

echo "Replacing the file with a second version..."
curl -fsS -u "${E2E_POOL_USER}:${E2E_POOL_PASS}" -X PUT \
  -H "If-Match: \"${FIRST_VERSION_ID}\"" --data-binary "@${SECOND_FILE}" \
  "http://127.0.0.1:${E2E_PORTS[0]}/dav/history.txt" >/dev/null
for index in 1 2; do
  e2e_wait_for_download "${index}" "/history.txt" "${SECOND_CONTENT}" || {
    echo "Node $((index + 1)) never received the second version" >&2
    exit 1
  }
done

e2e_stop_node 1
echo "Restoring the first version while node 2 is offline..."
curl -fsS -b "${E2E_COOKIES[0]}" -H "Content-Type: application/json" \
  -d "{\"file_id\":\"${FILE_ID}\",\"version_id\":\"${FIRST_VERSION_ID}\"}" \
  "http://127.0.0.1:${E2E_PORTS[0]}/api/files/versions/restore" >/dev/null
e2e_wait_for_download 2 "/history.txt" "${FIRST_CONTENT}" || {
  echo "Online node 3 did not receive the restored version" >&2
  exit 1
}

echo "Restarting node 2 and waiting for version restore convergence..."
e2e_start_node 1
e2e_login_node 1
e2e_wait_for_download 1 "/history.txt" "${FIRST_CONTENT}" || {
  echo "Recovered node 2 did not converge to the restored version" >&2
  exit 1
}

CURRENT_VERSIONS=()
for index in 0 1 2; do
  CURRENT_VERSIONS+=("$(curl -fsS -b "${E2E_COOKIES[index]}" "http://127.0.0.1:${E2E_PORTS[index]}/api/files?path=/" \
    | python3 -c 'import json,sys; print(next(x["version_id"] for x in json.load(sys.stdin)["data"]["entries"] if x["path"] == "/history.txt"))')")
done
if [[ "${CURRENT_VERSIONS[0]}" != "${CURRENT_VERSIONS[1]}" || "${CURRENT_VERSIONS[0]}" != "${CURRENT_VERSIONS[2]}" ]]; then
  echo "Nodes disagree on the restored current version: ${CURRENT_VERSIONS[*]}" >&2
  exit 1
fi

echo "SUCCESS"
echo "Artifacts retained: ${E2E_DIR}"
