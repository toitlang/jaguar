// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *
import host.directory
import host.file

import ..src.host.cli.config

main:
  test-round-trip
  test-invalid-keys

test-round-trip:
  temporary := directory.mkdtemp "/tmp/jaguar-config-test-"
  path := "$temporary/config.yaml"
  try:
    config := Config path
    expect-equals "missing" (config.get "wifi.ssid" --default="missing")
    config.set "wifi.ssid" "Open Wifi"
    config.set "wifi.password" "secret"
    config.set "cache.keep_old" true
    config.save

    expect (file.is-file path)
    loaded := Config path
    expect-equals "Open Wifi" (loaded.get "wifi.ssid")
    expect-equals "secret" (loaded.get "wifi.password")
    expect-equals true (loaded.get "cache.keep_old")
    expect (loaded.remove "wifi.password")
    expect-not (loaded.remove "wifi.password")
    loaded.save

    reloaded := Config path
    expect-equals null (reloaded.get "wifi.password")
    expect-equals "Open Wifi" (reloaded.get "wifi.ssid")
  finally:
    directory.rmdir --recursive temporary

test-invalid-keys:
  config := Config "/not-used" --data={:}
  expect-throw "Configuration key must not be empty": config.set "" 1
  expect-throw "Configuration key contains an empty component: 'a..b'":
    config.get "a..b"
