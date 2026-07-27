// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *

import ..src.network

main:
  expect-equals "office-sensor.local" (mdns-hostname "Office Sensor")
  expect-equals "sensor-2.local" (mdns-hostname "Sensor--2!")
  expect-equals "jaguar.local" (mdns-hostname "___")
  expect-equals "esp32.local" (mdns-hostname "-ESP32-")
  long-name := "sensor-" + ("x" * 80)
  hostname := mdns-hostname long-name
  expect-equals 69 hostname.size
  expect (hostname.ends-with ".local")
