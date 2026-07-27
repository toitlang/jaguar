#!/usr/bin/env bash

# Copyright (C) 2026 Toit contributors.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="${1:-}"
QEMU_SYSTEM_XTENSA="${QEMU_SYSTEM_XTENSA:-qemu-system-xtensa}"
TOIT_WIFI_ENVELOPE="${TOIT_WIFI_ENVELOPE:-}"
TOIT="${TOIT:-toit}"
QEMU_TIMEOUT_TICKS="${QEMU_TIMEOUT_TICKS:-600}"

case "${TARGET}" in
  esp32)
    MACHINE="esp32"
    CHIP="esp32"
    WDT_DRIVER="timer.esp32.timg"
    ;;
  esp32s3)
    MACHINE="esp32s3"
    CHIP="esp32s3"
    WDT_DRIVER="timer.esp32s3.timg"
    ;;
  *)
    echo "Usage: $0 {esp32|esp32s3}" >&2
    exit 2
    ;;
esac

if ! command -v "${QEMU_SYSTEM_XTENSA}" >/dev/null 2>&1 &&
    [[ ! -x "${QEMU_SYSTEM_XTENSA}" ]]; then
  echo "QEMU executable not found: ${QEMU_SYSTEM_XTENSA}" >&2
  exit 2
fi
if [[ ! -f "${TOIT_WIFI_ENVELOPE}" ]]; then
  echo "Set TOIT_WIFI_ENVELOPE to the matching ${TARGET} envelope." >&2
  exit 2
fi
TEMP_DIR="$(mktemp -d)"
QEMU_PID=""

cleanup() {
  if [[ -n "${QEMU_PID}" ]]; then
    kill "${QEMU_PID}" 2>/dev/null || true
    wait "${QEMU_PID}" 2>/dev/null || true
  fi
  rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

"${TOIT}" run "${ROOT_DIR}/tests/qemu/build-jaguar-envelope.toit" \
  "${TOIT_WIFI_ENVELOPE}" \
  "${CHIP}" \
  "${TEMP_DIR}/jaguar.envelope"

"${TOIT}" compile -Werror -O2 -s \
  -o "${TEMP_DIR}/device-probe.snapshot" \
  "${ROOT_DIR}/tests/qemu/device-probe.toit"
"${TOIT}" tool snapshot-to-image -m32 --format=binary \
  -o "${TEMP_DIR}/device-probe.image" \
  "${TEMP_DIR}/device-probe.snapshot"
"${TOIT}" tool firmware -e "${TEMP_DIR}/jaguar.envelope" container install \
  --output="${TEMP_DIR}/tested.envelope" \
  device-probe "${TEMP_DIR}/device-probe.image"

printf '%s\n' \
  '{"wifi":{"wifi.ssid":"Open Wifi","wifi.password":""}}' \
  >"${TEMP_DIR}/firmware-config.json"
"${TOIT}" tool firmware -e "${TEMP_DIR}/tested.envelope" extract \
  --format=image \
  --config="${TEMP_DIR}/firmware-config.json" \
  --output="${TEMP_DIR}/jaguar.bin"

"${QEMU_SYSTEM_XTENSA}" \
  -M "${MACHINE}" \
  -accel tcg,thread=single \
  -nographic \
  -no-reboot \
  -drive "file=${TEMP_DIR}/jaguar.bin,if=mtd,format=raw" \
  -global "driver=${WDT_DRIVER},property=wdt_disable,value=true" \
  -nic "user,model=esp32_wifi,net=192.168.4.0/24" \
  >"${TEMP_DIR}/qemu.log" 2>&1 &
QEMU_PID="$!"

PASSED=false
for ((tick = 0; tick < QEMU_TIMEOUT_TICKS; tick++)); do
  if grep -q '^JAGUAR-QEMU-DEVICE: PASS' "${TEMP_DIR}/qemu.log"; then
    PASSED=true
    break
  fi
  if ! kill -0 "${QEMU_PID}" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

cat "${TEMP_DIR}/qemu.log"
if [[ "${PASSED}" != true ]]; then
  echo "${TARGET} Jaguar device smoke test failed." >&2
  exit 1
fi
