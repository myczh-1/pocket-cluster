#!/usr/bin/env bash

# Shared lifecycle helpers for scenarios that run real agents on loopback.

e2e_require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "FAILED: required command '$1' is missing" >&2
    exit 1
  }
}

e2e_init() {
  local scenario=$1
  local nodes=$2
  local base_port=$3

  E2E_ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  E2E_DIR="$(mktemp -d "${TMPDIR:-/tmp}/pocketcluster-e2e-${scenario}.XXXXXX")"
  E2E_BIN="${E2E_DIR}/agent"
  E2E_NODE_COUNT=$nodes
  E2E_POOL_USER="${POOL_USER:-admin}"
  E2E_POOL_PASS="${POOL_PASS:-testpass}"
  E2E_PIDS=()
  E2E_PORTS=()
  E2E_DATA_DIRS=()
  E2E_COOKIES=()
  E2E_LOGS=()

  e2e_require_cmd curl
  e2e_require_cmd go
  e2e_require_cmd python3

  for ((i = 0; i < nodes; i++)); do
    E2E_PORTS[i]=$((base_port + i))
    E2E_DATA_DIRS[i]="${E2E_DIR}/node-$((i + 1))"
    E2E_COOKIES[i]="${E2E_DIR}/node-$((i + 1)).cookie"
    E2E_LOGS[i]="${E2E_DIR}/node-$((i + 1)).log"
    mkdir -p "${E2E_DATA_DIRS[i]}"
  done

  echo "E2E artifacts: ${E2E_DIR}"
  echo "Building agent binary..."
  go build -o "${E2E_BIN}" "${E2E_ROOT_DIR}/cmd/agent"
}

e2e_cleanup() {
  local exit_code=$?
  for pid in "${E2E_PIDS[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${E2E_PIDS[@]:-}"; do
    [[ -n "${pid}" ]] && wait "${pid}" >/dev/null 2>&1 || true
  done
  if [[ ${exit_code} -ne 0 ]]; then
    echo "FAILED"
    echo "Artifacts retained: ${E2E_DIR}"
    echo "Inspect node logs, metadata.db, and chunks under that directory."
  fi
}

e2e_start_node() {
  local index=$1
  local port=${E2E_PORTS[index]}
  echo "Starting node $((index + 1)) on port ${port}..."
  if [[ -f "${E2E_LOGS[index]}" ]]; then
    "${E2E_BIN}" -data "${E2E_DATA_DIRS[index]}" -port "${port}" -name "node-$((index + 1))" \
      -local-ip 127.0.0.1 -advertise-ip 127.0.0.1 >>"${E2E_LOGS[index]}" 2>&1 &
  else
    "${E2E_BIN}" -data "${E2E_DATA_DIRS[index]}" -port "${port}" -name "node-$((index + 1))" \
      -local-ip 127.0.0.1 -advertise-ip 127.0.0.1 >"${E2E_LOGS[index]}" 2>&1 &
  fi
  E2E_PIDS[index]=$!
  e2e_wait_for_health "${port}" || {
    echo "Node $((index + 1)) did not become healthy: ${E2E_LOGS[index]}" >&2
    exit 1
  }
}

e2e_stop_node() {
  local index=$1
  local pid=${E2E_PIDS[index]:-}
  [[ -n "${pid}" ]] || return 0
  echo "Stopping node $((index + 1))..."
  kill "${pid}" >/dev/null 2>&1 || true
  wait "${pid}" >/dev/null 2>&1 || true
  E2E_PIDS[index]=""
}

e2e_wait_for_health() {
  local port=$1
  for _ in $(seq 1 45); do
    if curl -fsS "http://127.0.0.1:${port}/api/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

e2e_create_cluster() {
  curl -fsS -c "${E2E_COOKIES[0]}" -H "Content-Type: application/json" \
    -d "{\"username\":\"${E2E_POOL_USER}\",\"password\":\"${E2E_POOL_PASS}\"}" \
    "http://127.0.0.1:${E2E_PORTS[0]}/api/cluster" >/dev/null
}

e2e_join_node() {
  local index=$1
  local node_id
  local join_output="${E2E_DIR}/join-node-$((index + 1)).json"
  node_id="$(curl -fsS "http://127.0.0.1:${E2E_PORTS[index]}/api/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["node_id"])')"

  curl -fsS -o "${join_output}" -H "Content-Type: application/json" \
    -d "{\"bootstrap\":\"http://127.0.0.1:${E2E_PORTS[0]}\",\"pool_user\":\"${E2E_POOL_USER}\",\"pool_password\":\"${E2E_POOL_PASS}\"}" \
    "http://127.0.0.1:${E2E_PORTS[index]}/api/join" &
  local join_pid=$!

  for _ in $(seq 1 20); do
    if curl -fsS -b "${E2E_COOKIES[0]}" "http://127.0.0.1:${E2E_PORTS[0]}/api/join/pending" | grep -q "${node_id}"; then
      break
    fi
    sleep 1
  done
  curl -fsS -b "${E2E_COOKIES[0]}" -X POST \
    "http://127.0.0.1:${E2E_PORTS[0]}/api/join/approve/${node_id}" >/dev/null
  wait "${join_pid}"

  e2e_login_node "${index}"
}

e2e_login_node() {
  local index=$1
  curl -fsS -c "${E2E_COOKIES[index]}" -H "Content-Type: application/json" \
    -d "{\"username\":\"${E2E_POOL_USER}\",\"password\":\"${E2E_POOL_PASS}\"}" \
    "http://127.0.0.1:${E2E_PORTS[index]}/api/auth/login" >/dev/null
}

e2e_start_cluster() {
  local count=$1
  for ((i = 0; i < count; i++)); do e2e_start_node "${i}"; done
  echo "Creating cluster on node 1..."
  e2e_create_cluster
  for ((i = 1; i < count; i++)); do
    echo "Joining node $((i + 1))..."
    e2e_join_node "${i}"
  done
}

e2e_upload() {
  local index=$1
  local pool_path=$2
  local source_file=$3
  curl -fsS -b "${E2E_COOKIES[index]}" -F "path=${pool_path}" -F "file=@${source_file}" \
    "http://127.0.0.1:${E2E_PORTS[index]}/api/files/upload" >/dev/null
}

e2e_webdav_put() {
  local index=$1
  local pool_path=$2
  local source_file=$3
  local etag=${4:-}
  local -a command=(curl -fsS -u "${E2E_POOL_USER}:${E2E_POOL_PASS}")
  if [[ -n "${etag}" ]]; then
    command+=(-H "If-Match: ${etag}")
  fi
  command+=(-T "${source_file}" "http://127.0.0.1:${E2E_PORTS[index]}/dav${pool_path}")
  "${command[@]}" >/dev/null
}

e2e_webdav_etag() {
  local index=$1
  local pool_path=$2
  curl -fsSI -u "${E2E_POOL_USER}:${E2E_POOL_PASS}" \
    "http://127.0.0.1:${E2E_PORTS[index]}/dav${pool_path}" \
    | awk 'tolower($1) == "etag:" { gsub("\\r", ""); print $2; exit }'
}

e2e_wait_for_webdav_etag() {
  local index=$1
  local pool_path=$2
  for _ in $(seq 1 45); do
    local etag
    etag="$(e2e_webdav_etag "${index}" "${pool_path}" 2>/dev/null || true)"
    if [[ -n "${etag}" ]]; then
      printf '%s\n' "${etag}"
      return 0
    fi
    sleep 1
  done
  return 1
}

e2e_rename() {
  local index=$1
  local old_path=$2
  local new_path=$3
  curl -fsS -b "${E2E_COOKIES[index]}" -X PATCH -H "Content-Type: application/json" \
    -d "{\"path\":\"${old_path}\",\"new_path\":\"${new_path}\"}" \
    "http://127.0.0.1:${E2E_PORTS[index]}/api/files/rename" >/dev/null
}

e2e_wait_for_download() {
  local index=$1
  local pool_path=$2
  local expected=$3
  for _ in $(seq 1 45); do
    local body
    body="$(curl -fsS -b "${E2E_COOKIES[index]}" "http://127.0.0.1:${E2E_PORTS[index]}/api/files/download?path=${pool_path}" 2>/dev/null || true)"
    if [[ "${body}" == "${expected}" ]]; then return 0; fi
    sleep 1
  done
  return 1
}

e2e_wait_for_not_found() {
  local index=$1
  local pool_path=$2
  for _ in $(seq 1 45); do
    local status
    status="$(curl -sS -o /dev/null -w '%{http_code}' -b "${E2E_COOKIES[index]}" "http://127.0.0.1:${E2E_PORTS[index]}/api/files/download?path=${pool_path}" || true)"
    if [[ "${status}" == "404" ]]; then return 0; fi
    sleep 1
  done
  return 1
}

e2e_start_purge() {
  local index=$1
  curl -fsS -b "${E2E_COOKIES[index]}" -X POST "http://127.0.0.1:${E2E_PORTS[index]}/api/jobs/purge-retained-data" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["id"])'
}

e2e_wait_for_job() {
  local index=$1
  local job_id=$2
  for _ in $(seq 1 45); do
    local status
    status="$(curl -fsS -b "${E2E_COOKIES[index]}" "http://127.0.0.1:${E2E_PORTS[index]}/api/jobs/${job_id}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["status"])')"
    if [[ "${status}" == "done" ]]; then return 0; fi
    if [[ "${status}" == "failed" || "${status}" == "blocked" ]]; then
      echo "Job ${job_id} finished as ${status}" >&2
      return 1
    fi
    sleep 1
  done
  return 1
}

e2e_chunk_count() {
  local index=$1
  find "${E2E_DATA_DIRS[index]}/chunks" -type f | wc -l | tr -d ' '
}
