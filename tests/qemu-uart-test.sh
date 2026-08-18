#!/usr/bin/env bash

# Copyright (C) 2026 Toit contributors.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JAG="${JAG:-${ROOT_DIR}/build/jag}"
QEMU_SYSTEM_XTENSA="${QEMU_SYSTEM_XTENSA:-qemu-system-xtensa}"
QEMU_TIMEOUT_TICKS="${QEMU_TIMEOUT_TICKS:-300}"
EXPECTED_BAUD_RATE=921600

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
MONITOR_PID=""

stop_monitor() {
  if [[ -n "${MONITOR_PID}" ]]; then
    kill "${MONITOR_PID}" 2>/dev/null || true
    wait "${MONITOR_PID}" 2>/dev/null || true
    MONITOR_PID=""
  fi
}

stop_qemu() {
  if [[ -n "${QEMU_PID}" ]]; then
    kill "${QEMU_PID}" 2>/dev/null || true
    wait "${QEMU_PID}" 2>/dev/null || true
    QEMU_PID=""
  fi
}

cleanup() {
  stop_monitor
  stop_qemu
  rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

"${JAG}" \
  --no-analytics \
  firmware extract esp32 \
  --wifi-ssid="Open Wifi" --wifi-password= \
  --name=qemu-uart-test \
  --disable-udp \
  --output="${TEMP_DIR}/firmware.bin"

run_proxy() {
  local name="$1"
  local expect_switch="$2"
  shift 2

  local image="${TEMP_DIR}/${name}.bin"
  local qemu_log="${TEMP_DIR}/${name}.qemu.log"
  local monitor_log="${TEMP_DIR}/${name}.monitor.log"
  cp "${TEMP_DIR}/firmware.bin" "${image}"

  "${QEMU_SYSTEM_XTENSA}" \
    -M esp32 \
    -accel tcg,thread=single \
    -display none \
    -monitor none \
    -no-reboot \
    -serial pty \
    -drive "file=${image},if=mtd,format=raw" \
    >"${qemu_log}" 2>&1 &
  QEMU_PID="$!"

  local serial_port=""
  for ((tick = 0; tick < QEMU_TIMEOUT_TICKS; tick++)); do
    serial_port="$(awk '/char device redirected to/ { print $5; exit }' "${qemu_log}")"
    if [[ -n "${serial_port}" ]]; then
      break
    fi
    if ! kill -0 "${QEMU_PID}" 2>/dev/null; then
      break
    fi
    sleep 0.1
  done

  if [[ -z "${serial_port}" ]]; then
    echo "Timed out waiting for QEMU's serial port." >&2
    cat "${qemu_log}" >&2
    exit 1
  fi

  "${JAG}" \
    --no-analytics \
    monitor \
    --attach \
    --force-plain \
    --proxy \
    --port="${serial_port}" \
    "$@" \
    >"${monitor_log}" 2>&1 &
  MONITOR_PID="$!"

  for ((tick = 0; tick < QEMU_TIMEOUT_TICKS; tick++)); do
    if grep -a -Fq "proxied through" "${monitor_log}" 2>/dev/null; then
      break
    fi
    if ! kill -0 "${QEMU_PID}" 2>/dev/null || ! kill -0 "${MONITOR_PID}" 2>/dev/null; then
      break
    fi
    sleep 0.1
  done

  if ! grep -a -Fq "proxied through" "${monitor_log}"; then
    echo "Jaguar did not start its UART proxy in QEMU (${name})." >&2
    cat "${qemu_log}" >&2
    cat "${monitor_log}" >&2
    exit 1
  fi

  if [[ "${expect_switch}" == true ]]; then
    if ! grep -a -Fq "switched UART proxy to ${EXPECTED_BAUD_RATE} baud" "${monitor_log}"; then
      echo "Jaguar did not confirm the ${EXPECTED_BAUD_RATE}-baud transition." >&2
      cat "${monitor_log}" >&2
      exit 1
    fi
  elif grep -a -Fq "switched UART proxy" "${monitor_log}"; then
    echo "Jaguar changed the baud rate despite an explicit --baud option." >&2
    cat "${monitor_log}" >&2
    exit 1
  fi

  stop_monitor
  stop_qemu
}

run_proxy default true
run_proxy explicit false --baud=115200

echo "PASS: Jaguar proxied its console UART with default and explicit baud rates in QEMU"
