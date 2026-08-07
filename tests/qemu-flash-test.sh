#!/usr/bin/env bash

# Copyright (C) 2026 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JAG="${JAG:-${ROOT_DIR}/build/jag}"
QEMU_SYSTEM_XTENSA="${QEMU_SYSTEM_XTENSA:-qemu-system-xtensa}"
QEMU_TIMEOUT_TICKS="${QEMU_TIMEOUT_TICKS:-300}"

if [[ ! -x "${JAG}" ]]; then
  echo "Jaguar executable is not executable: ${JAG}" >&2
  exit 2
fi
if ! command -v "${QEMU_SYSTEM_XTENSA}" >/dev/null 2>&1; then
  echo "QEMU executable not found: ${QEMU_SYSTEM_XTENSA}" >&2
  exit 2
fi

TEMP_DIR="$(mktemp -d)"
QEMU_PID=""
QEMU_LOG="${TEMP_DIR}/qemu.log"

cleanup() {
  if [[ -n "${QEMU_PID}" ]]; then
    kill "${QEMU_PID}" 2>/dev/null || true
    wait "${QEMU_PID}" 2>/dev/null || true
  fi
  rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

# Download/cache the real published envelope, and verify the SDK provided by
# `jag setup` has the bundled esptool this test is intended to exercise.
SDK_PATH="$("${JAG}" setup --print-path sdk)"
BUNDLED_ESPTOOL="${SDK_PATH}/lib/toit/bin/esptool"
if [[ ! -x "${BUNDLED_ESPTOOL}" ]]; then
  echo "Bundled esptool is not executable: ${BUNDLED_ESPTOOL}" >&2
  exit 2
fi
if [[ "$("${BUNDLED_ESPTOOL}" version)" != *"esptool v5."* ]]; then
  echo "The bundled esptool is not version 5." >&2
  exit 2
fi
"${JAG}" \
  --no-analytics \
  --wifi-ssid=test --wifi-password=test \
  firmware extract esp32 \
  --chip=esp32 \
  --exclude-jaguar \
  --output="${TEMP_DIR}/base.bin"

# Recreate the source-tree layout selected by JAG_TOIT_REPO_PATH using the
# actual downloaded SDK and envelope. This is the configuration from the bug
# report: it must still select the SDK's bundle, not a Python esptool module.
TOIT_REPO_PATH="${TEMP_DIR}/toit"
mkdir -p "${TOIT_REPO_PATH}/build/host" "${TOIT_REPO_PATH}/build/esp32"
ln -s "${SDK_PATH}" "${TOIT_REPO_PATH}/build/host/sdk"
ln -s "$(dirname "${SDK_PATH}")/envelopes/firmware-esp32.envelope" \
  "${TOIT_REPO_PATH}/build/esp32/firmware.envelope"
export JAG_TOIT_REPO_PATH="${TOIT_REPO_PATH}"

# A 4 MiB flash drive matches the default ESP32 firmware envelope. Strap mode
# 0x0f starts the ESP32 ROM UART downloader instead of booting from flash.
truncate --size=4M "${TEMP_DIR}/flash.bin"
"${QEMU_SYSTEM_XTENSA}" \
  -M esp32 \
  -accel tcg,thread=single \
  -global driver=esp32.gpio,property=strap_mode,value=15 \
  -display none \
  -monitor none \
  -no-reboot \
  -serial pty \
  -drive "file=${TEMP_DIR}/flash.bin,if=mtd,format=raw" \
  >"${QEMU_LOG}" 2>&1 &
QEMU_PID="$!"

SERIAL_PORT=""
for ((tick = 0; tick < QEMU_TIMEOUT_TICKS; tick++)); do
  SERIAL_PORT="$(awk '/char device redirected to/ { print $5; exit }' "${QEMU_LOG}")"
  if [[ -n "${SERIAL_PORT}" ]]; then
    break
  fi
  if ! kill -0 "${QEMU_PID}" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

if [[ -z "${SERIAL_PORT}" ]]; then
  echo "Timed out waiting for QEMU's serial port." >&2
  cat "${QEMU_LOG}" >&2
  exit 1
fi

"${JAG}" \
  --no-analytics \
  --wifi-ssid=test --wifi-password=test \
  flash \
  --chip=esp32 \
  --port="${SERIAL_PORT}" \
  --skip-port-check \
  --exclude-jaguar

echo "PASS: Jaguar flashed an ESP32 in QEMU with its SDK's bundled esptool"
