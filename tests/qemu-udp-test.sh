#!/usr/bin/env bash

# Copyright (C) 2026 Toit contributors.
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

stop_qemu() {
  if [[ -n "${QEMU_PID}" ]]; then
    kill "${QEMU_PID}" 2>/dev/null || true
    wait "${QEMU_PID}" 2>/dev/null || true
    QEMU_PID=""
  fi
}

cleanup() {
  stop_qemu
  rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

build_image() {
  local output="$1"
  shift
  "${JAG}" \
    --no-analytics \
    firmware extract esp32-qemu \
    --wifi-ssid=test --wifi-password=test \
    --name=qemu-udp-test \
    --output="${output}" \
    "$@"
}

run_image() {
  local image="$1"
  local log="$2"
  local capture="$3"

  "${QEMU_SYSTEM_XTENSA}" \
    -M esp32 \
    -accel tcg,thread=single \
    -display none \
    -monitor none \
    -no-reboot \
    -serial "file:${log}" \
    -drive "file=${image},if=mtd,format=raw" \
    -netdev user,id=network \
    -device open_eth,netdev=network \
    -object "filter-dump,id=capture,netdev=network,file=${capture}" \
    >/dev/null 2>&1 &
  QEMU_PID="$!"

  for ((tick = 0; tick < QEMU_TIMEOUT_TICKS; tick++)); do
    if grep -a -Fq "running Jaguar device" "${log}" 2>/dev/null; then
      # Identity packets are sent every 200ms. Allow several opportunities
      # for an advertisement to reach the packet capture.
      sleep 2
      stop_qemu
      return
    fi
    if ! kill -0 "${QEMU_PID}" 2>/dev/null; then
      break
    fi
    sleep 0.1
  done

  stop_qemu
  echo "Timed out waiting for Jaguar to start in QEMU." >&2
  cat "${log}" >&2
  exit 1
}

build_image "${TEMP_DIR}/udp-enabled.bin"
build_image "${TEMP_DIR}/udp-disabled.bin" --disable-udp

run_image \
  "${TEMP_DIR}/udp-enabled.bin" \
  "${TEMP_DIR}/udp-enabled.log" \
  "${TEMP_DIR}/udp-enabled.pcap"
run_image \
  "${TEMP_DIR}/udp-disabled.bin" \
  "${TEMP_DIR}/udp-disabled.log" \
  "${TEMP_DIR}/udp-disabled.pcap"

if ! grep -a -Fq "jaguar.identify" "${TEMP_DIR}/udp-enabled.pcap"; then
  echo "Jaguar did not advertise its identity without --disable-udp." >&2
  exit 1
fi
if grep -a -Fq "jaguar.identify" "${TEMP_DIR}/udp-disabled.pcap"; then
  echo "Jaguar advertised its identity with --disable-udp." >&2
  exit 1
fi

echo "PASS: --disable-udp suppresses Jaguar UDP advertisements in QEMU"
