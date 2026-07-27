// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *

import ..src.host.cli.commands
import ..src.host.cli.device-client

main:
  parsed := parse-defines [
    "count=42",
    "enabled=true",
    "label=hello",
    "jag.wifi=false",
    "jag.timeout=1500ms",
    "jag.interval=3s",
  ]
  expect-equals 42 parsed.assets["count"]
  expect-equals true parsed.assets["enabled"]
  expect-equals "hello" parsed.assets["label"]
  expect-equals "true" parsed.headers[HEADER-WIFI-DISABLED]
  expect-equals "2" parsed.headers[HEADER-CONTAINER-TIMEOUT]
  expect-equals "3s" parsed.headers[HEADER-CONTAINER-INTERVAL]

  expect-throw "Unsupported Jaguar define: 'jag.unknown'":
    parse-defines ["jag.unknown=true"]
