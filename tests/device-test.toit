// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *

import ..src.host.cli.device

IDENTITY ::= """
  {
    "method": "jaguar.identify",
    "payload": {
      "name": "kitchen",
      "id": "0f9f731d-f7aa-4e15-b8f7-587b4aaad123",
      "chip": "esp32",
      "sdkVersion": "v2.0.0-alpha.196",
      "address": "http://192.168.4.3:9000",
      "wordSize": 4
    }
  }
  """.to-byte-array

main:
  device := Device.from-udp IDENTITY
  expect-not-null device
  expect-equals "kitchen" device.name
  expect-equals "esp32" device.chip
  expect-equals 4 device.word-size
  expect (device.matches "kitchen")
  expect (device.matches device.id)
  expect (device.matches "http://192.168.4.3:9000")
  expect (device.discoveries.contains "udp")

  expect-equals null (Device.from-udp "{}".to-byte-array)
  expect-equals null (Device.from-udp "not JSON".to-byte-array)

  mdns := Device.from-identity device.to-map --discovery="mdns"
  mdns.address = "http://kitchen.local:9000"
  device.merge mdns
  expect (device.discoveries.contains "udp")
  expect (device.discoveries.contains "mdns")
  expect-equals "http://kitchen.local:9000" device.address
