#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web_version="$(node -p "require('${ROOT_DIR}/web/package.json').version")"
android_version="$(sed -n 's/.*versionName = "\([^"]*\)".*/\1/p' "${ROOT_DIR}/android/app/build.gradle.kts" | head -1)"

if [[ -z "${web_version}" || -z "${android_version}" ]]; then
  echo "Could not read the Web or Android version."
  exit 1
fi
if [[ "${web_version}" != "${android_version}" ]]; then
  echo "Version mismatch: Web=${web_version}, Android=${android_version}."
  exit 1
fi
if [[ "${GITHUB_REF_TYPE:-}" == "tag" && "${GITHUB_REF_NAME:-}" != "v${web_version}" ]]; then
  echo "Release tag ${GITHUB_REF_NAME} does not match v${web_version}."
  exit 1
fi

echo "Version ${web_version} is consistent."
