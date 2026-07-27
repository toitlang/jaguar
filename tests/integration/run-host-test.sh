#!/usr/bin/env bash

# Copyright (C) 2026 Toit contributors.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
JAG_BINARY="${JAG_BINARY:-${ROOT_DIR}/build/jag}"
PORT="${JAG_HOST_TEST_PORT:-19001}"
TEMP_DIR="$(mktemp -d)"
SIMULATOR_PID=""

cleanup() {
  if [[ -n "${SIMULATOR_PID}" ]]; then
    kill "${SIMULATOR_PID}" 2>/dev/null || true
    wait "${SIMULATOR_PID}" 2>/dev/null || true
  fi
  rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

export JAG_USER_CONFIG_PATH="${TEMP_DIR}/user.yaml"
export JAG_DEVICE_CONFIG_PATH="${TEMP_DIR}/device.yaml"
export JAG_SNAPSHOT_CACHE_PATH="${TEMP_DIR}/snapshots"
export JAG_CACHE_DIR="${TEMP_DIR}/cache"

cd "${ROOT_DIR}"
"${JAG_BINARY}" simulate --port="${PORT}" --name=host-sim \
  >"${TEMP_DIR}/simulator.log" 2>&1 &
SIMULATOR_PID="$!"

ADDRESS="127.0.0.1:${PORT}"
READY=false
for _ in {1..100}; do
  if curl --fail --silent --max-time 0.2 \
      "http://${ADDRESS}/identify" >/dev/null 2>&1; then
    READY=true
    break
  fi
  if ! kill -0 "${SIMULATOR_PID}" 2>/dev/null; then
    break
  fi
  sleep 0.1
done
if [[ "${READY}" != true ]]; then
  cat "${TEMP_DIR}/simulator.log"
  echo "Host Jaguar simulator did not become ready." >&2
  exit 1
fi

"${JAG_BINARY}" scan "${ADDRESS}" --output=json \
  | grep -q '"name":"host-sim"'
"${JAG_BINARY}" scan --discovery=mdns --list --timeout=3s --output=json \
  | grep -q '"name":"host-sim"'
"${JAG_BINARY}" ping --device="${ADDRESS}" \
  | grep -q '^Got pong from host-sim'
"${JAG_BINARY}" run --device="${ADDRESS}" \
  tests/qemu/hello.toit
"${JAG_BINARY}" run --device="${ADDRESS}" \
  -D integration.value=42 \
  tests/integration/defines.toit
"${JAG_BINARY}" container install --device="${ADDRESS}" \
  smoke tests/qemu/hello.toit
"${JAG_BINARY}" container list --device="${ADDRESS}" \
  >"${TEMP_DIR}/containers.log"
grep -q $'\tsmoke$' "${TEMP_DIR}/containers.log"
"${JAG_BINARY}" container uninstall --device="${ADDRESS}" smoke
"${JAG_BINARY}" container list --device="${ADDRESS}" \
  >"${TEMP_DIR}/containers-after.log"
if grep -q $'\tsmoke$' "${TEMP_DIR}/containers-after.log"; then
  echo "Container still installed after uninstall." >&2
  exit 1
fi
grep -q '^JAGUAR-QEMU-COMMAND: PASS' "${TEMP_DIR}/simulator.log"
grep -q '^JAGUAR-DEFINES: 42' "${TEMP_DIR}/simulator.log"
