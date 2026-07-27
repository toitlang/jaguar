#!/bin/sh

# Copyright (C) 2026 Toit contributors.

set -eu

repro_dir="$(mktemp -d /tmp/toit-pkg-lock-repro.XXXXXX)"
trap 'rm -rf "$repro_dir"' EXIT

toit pkg init \
  --project-root "$repro_dir" \
  --name lock-repro \
  --auto-sync=false

# This is the state left when a package-manager process is killed while it
# owns the per-project package-cache lock.
mkdir -p "$repro_dir/.packages/.lock"
sleep 2

cd "$repro_dir"
if output="$(toit pkg install certificate-roots --auto-sync=false 2>&1)"; then
  echo "Expected package installation to fail with LOCK_STALE" >&2
  exit 1
fi

printf '%s\n' "$output"
case "$output" in
  *LOCK_STALE*)
    echo "Reproduced LOCK_STALE"
    ;;
  *)
    echo "Package installation failed, but not with LOCK_STALE" >&2
    exit 1
    ;;
esac
