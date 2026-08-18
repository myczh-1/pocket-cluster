#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APK_PATH="${APK_PATH:-${ROOT_DIR}/android/app/build/outputs/apk/debug/app-debug.apk}"
PACKAGE="com.pocketcluster.agent"
LOCAL_PORT="${LOCAL_PORT:-17788}"
ADB_BIN="${ADB_BIN:-$(command -v adb || true)}"
if [[ -z "${ADB_BIN}" && -f "${ROOT_DIR}/android/local.properties" ]]; then
  sdk_dir="$(sed -n 's/^sdk.dir=//p' "${ROOT_DIR}/android/local.properties" | head -1)"
  if [[ -x "${sdk_dir}/platform-tools/adb" ]]; then
    ADB_BIN="${sdk_dir}/platform-tools/adb"
  fi
fi

[[ -x "${ADB_BIN}" ]] || { echo "adb is required; set ADB_BIN or Android SDK path in android/local.properties"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required"; exit 1; }

if [[ -z "${ANDROID_SERIAL:-}" ]]; then
  devices="$("${ADB_BIN}" devices | awk 'NR > 1 && $2 == "device" { print $1 }')"
  device_count="$(printf '%s\n' "${devices}" | awk 'NF { count++ } END { print count+0 }')"
  if [[ "${device_count}" -ne 1 ]]; then
    echo "Connect exactly one authorized Android device or set ANDROID_SERIAL."
    exit 1
  fi
  export ANDROID_SERIAL="${devices}"
fi

if [[ -f "${APK_PATH}" ]]; then
  "${ADB_BIN}" install -r "${APK_PATH}" >/dev/null
fi
"${ADB_BIN}" shell pm grant "${PACKAGE}" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
"${ADB_BIN}" shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
"${ADB_BIN}" shell wm dismiss-keyguard >/dev/null 2>&1 || true
"${ADB_BIN}" shell cmd statusbar collapse >/dev/null 2>&1 || true
"${ADB_BIN}" shell am force-stop "${PACKAGE}" >/dev/null
"${ADB_BIN}" shell am start -n "${PACKAGE}/.MainActivity" >/dev/null
"${ADB_BIN}" shell cmd statusbar collapse >/dev/null 2>&1 || true
sleep 2

"${ADB_BIN}" shell uiautomator dump /sdcard/pocketcluster-window.xml >/dev/null
toggle_node="$("${ADB_BIN}" shell cat /sdcard/pocketcluster-window.xml | tr '>' '\n' | grep 'resource-id="com.pocketcluster.agent:id/btnToggle"' | head -1 || true)"
if [[ -z "${toggle_node}" ]]; then
  echo "PocketCluster is covered by the lock screen or another system window. Unlock the device and rerun."
  exit 1
fi
if [[ "${toggle_node}" == *'text="START AGENT"'* ]]; then
  bounds="$(printf '%s' "${toggle_node}" | sed -E 's/.*bounds="\[([0-9]+),([0-9]+)\]\[([0-9]+),([0-9]+)\]".*/\1 \2 \3 \4/')"
  read -r left top right bottom <<<"${bounds}"
  "${ADB_BIN}" shell input tap "$(((left + right) / 2))" "$(((top + bottom) / 2))"
fi

"${ADB_BIN}" forward "tcp:${LOCAL_PORT}" tcp:7788 >/dev/null
cleanup() { "${ADB_BIN}" forward --remove "tcp:${LOCAL_PORT}" >/dev/null 2>&1 || true; }
trap cleanup EXIT

for _ in $(seq 1 30); do
  if curl --noproxy '*' -fsS "http://127.0.0.1:${LOCAL_PORT}/api/health" >/dev/null 2>&1; then
    echo "Android agent health check passed on ${ANDROID_SERIAL}."
    "${ADB_BIN}" logcat -d -s AgentService AndroidNsdDiscovery | tail -80
    exit 0
  fi
  sleep 1
done

echo "Android agent did not become healthy within 30 seconds."
"${ADB_BIN}" logcat -d -s AgentService AndroidNsdDiscovery | tail -120
exit 1
