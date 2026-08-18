#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

scenario_state() {
  local index=$1
  python3 - "${E2E_DATA_DIRS[index]}/metadata.db" <<'PY'
import json
import sqlite3
import sys

db = sqlite3.connect(sys.argv[1])
rows = db.execute("""
    SELECT path, file_id, chunk_ids, version_id, parent_version_id, conflict_of, deleted
    FROM files
    WHERE path = '/docs/report.txt' OR path LIKE '/docs/report.sync-conflict-%'
    ORDER BY path
""").fetchall()
print(json.dumps([
    {
        "path": path,
        "file_id": file_id,
        "content_digest": json.loads(chunk_ids),
        "version_id": version_id,
        "parent_version_id": parent_version_id,
        "conflict_of": conflict_of,
        "deleted": bool(deleted),
    }
    for path, file_id, chunk_ids, version_id, parent_version_id, conflict_of, deleted in rows
], sort_keys=True, separators=(",", ":")))
PY
}

wait_for_convergence() {
  local expected_main=$1
  local previous=""
  for _ in $(seq 1 60); do
    local a b c
    a="$(scenario_state 0)"
    b="$(scenario_state 1)"
    c="$(scenario_state 2)"
    if [[ "${a}" == "${b}" && "${b}" == "${c}" ]] \
      && e2e_wait_for_download 0 "/docs/report.txt" "${expected_main}" \
      && e2e_wait_for_download 1 "/docs/report.txt" "${expected_main}" \
      && e2e_wait_for_download 2 "/docs/report.txt" "${expected_main}"; then
      if [[ "${a}" == "${previous}" ]]; then
        printf '%s\n' "${a}"
        return 0
      fi
      previous="${a}"
    fi
    sleep 1
  done
  echo "Nodes did not converge to ${expected_main}" >&2
  for index in 0 1 2; do
    echo "node $((index + 1)): $(scenario_state "${index}")" >&2
  done
  return 1
}

assert_one_main_and_conflict() {
  local state=$1
  local require_shared_parent=${2:-false}
  python3 - "${state}" "${require_shared_parent}" <<'PY'
import json
import sys

rows = json.loads(sys.argv[1])
require_shared_parent = sys.argv[2] == "true"
main = [row for row in rows if row["path"] == "/docs/report.txt" and not row["deleted"]]
conflicts = [row for row in rows if ".sync-conflict-" in row["path"] and not row["deleted"]]
if len(main) != 1 or len(conflicts) != 1:
    raise SystemExit(f"expected one main file and one conflict copy, got {rows}")
if conflicts[0]["conflict_of"] != main[0]["file_id"]:
    raise SystemExit(f"conflict relationship is invalid: {rows}")
if require_shared_parent and (main[0]["parent_version_id"] == "" or conflicts[0]["parent_version_id"] != main[0]["parent_version_id"]):
    raise SystemExit(f"concurrent versions do not share their parent: {rows}")
if main[0]["content_digest"] == conflicts[0]["content_digest"]:
    raise SystemExit(f"conflict copy lost one concurrent content: {rows}")
PY
}

run_case() {
  local case_name=$1 first=$2 second=$3 base_port=$4
  e2e_init "three-node-offline-concurrent-${case_name}" 3 "${base_port}"
  e2e_start_cluster 3

  local base_file="${E2E_DIR}/base.txt" a_file="${E2E_DIR}/from-a.txt" b_file="${E2E_DIR}/from-b.txt" after_file="${E2E_DIR}/after.txt"
  local base_content="base-${case_name}" content_a="content-from-A-${case_name}" content_b="content-from-B-${case_name}" after_content="content-after-resolution-${case_name}"
  printf '%s\n' "${base_content}" >"${base_file}"
  printf '%s\n' "${content_a}" >"${a_file}"
  printf '%s\n' "${content_b}" >"${b_file}"
  printf '%s\n' "${after_content}" >"${after_file}"

  echo "[${case_name}] creating and replicating the base version..."
  curl -fsS -u "${E2E_POOL_USER}:${E2E_POOL_PASS}" -X MKCOL "http://127.0.0.1:${E2E_PORTS[0]}/dav/docs" >/dev/null
  e2e_webdav_put 0 "/docs/report.txt" "${base_file}"
  for index in 1 2; do
    e2e_wait_for_download "${index}" "/docs/report.txt" "${base_content}" || return 1
  done
  local base_etag
  base_etag="$(e2e_wait_for_webdav_etag 0 "/docs/report.txt")"
  [[ -n "${base_etag}" ]] || { echo "[${case_name}] missing base ETag" >&2; return 1; }

  e2e_stop_node 1
  e2e_stop_node 2
  echo "[${case_name}] writing A's offline version..."
  e2e_webdav_put 0 "/docs/report.txt" "${a_file}" "${base_etag}"
  local a_version
  a_version="$(scenario_state 0 | python3 -c 'import json,sys; print(next(row["version_id"] for row in json.load(sys.stdin) if row["path"] == "/docs/report.txt"))')"
  e2e_stop_node 0

  e2e_start_node 1
  e2e_login_node 1
  echo "[${case_name}] writing B's offline version..."
  e2e_webdav_put 1 "/docs/report.txt" "${b_file}" "${base_etag}"
  local b_version
  b_version="$(scenario_state 1 | python3 -c 'import json,sys; print(next(row["version_id"] for row in json.load(sys.stdin) if row["path"] == "/docs/report.txt"))')"
  e2e_stop_node 1

  e2e_start_node 2
  e2e_login_node 2
  e2e_start_node "${first}"
  e2e_login_node "${first}"
  sleep 2
  e2e_start_node "${second}"
  e2e_login_node "${second}"

  local expected_main
  if [[ "${a_version}" < "${b_version}" ]]; then expected_main="${content_a}"; else expected_main="${content_b}"; fi
  local converged
  converged="$(wait_for_convergence "${expected_main}")" || return 1
  assert_one_main_and_conflict "${converged}" true

  echo "[${case_name}] restarting all nodes after convergence..."
  for index in 0 1 2; do e2e_stop_node "${index}"; done
  for index in 0 1 2; do e2e_start_node "${index}"; e2e_login_node "${index}"; done
  local restarted
  restarted="$(wait_for_convergence "${expected_main}")" || return 1
  [[ "${restarted}" == "${converged}" ]] || { echo "[${case_name}] restart changed normalized metadata" >&2; return 1; }

  echo "[${case_name}] continuing the resolved history from node C..."
  local current_etag
  current_etag="$(e2e_wait_for_webdav_etag 2 "/docs/report.txt")" || { echo "[${case_name}] main file ETag did not recover" >&2; return 1; }
  e2e_webdav_put 2 "/docs/report.txt" "${after_file}" "${current_etag}"
  local continued
  continued="$(wait_for_convergence "${after_content}")" || return 1
  assert_one_main_and_conflict "${continued}"
  echo "[${case_name}] SUCCESS"
  e2e_cleanup
}

trap e2e_cleanup EXIT
case "${E2E_CASE:-both}" in
  recover-a-then-b)
    run_case "recover-a-then-b" 0 1 "${E2E_BASE_PORT:-17830}"
    ;;
  recover-b-then-a)
    run_case "recover-b-then-a" 1 0 "${E2E_BASE_PORT:-17830}"
    ;;
  both)
    run_case "recover-a-then-b" 0 1 "${E2E_BASE_PORT:-17830}"
    run_case "recover-b-then-a" 1 0 "$(( ${E2E_BASE_PORT:-17830} + 10 ))"
    ;;
  *)
    echo "Unknown E2E_CASE: ${E2E_CASE}" >&2
    exit 2
    ;;
esac
echo "SUCCESS"
