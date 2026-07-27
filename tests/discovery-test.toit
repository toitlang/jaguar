// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *

import ..src.host.cli.device
import ..src.host.cli.discovery

class FakeDiscovery implements Discovery:
  name/string
  devices/List
  fails/bool

  constructor .name .devices --.fails=false:

  scan --timeout/Duration -> List:
    if fails: throw "discovery failed"
    return devices

main:
  udp := Device
      --name="sensor"
      --id="11111111-2222-4333-8444-555555555555"
      --chip="esp32"
      --sdk-version="v2.0.0-alpha.196"
      --address="http://192.168.4.2:9000"
      --word-size=4
      --discoveries={"udp"}
  mdns := Device
      --name="sensor"
      --id=udp.id
      --chip="esp32"
      --sdk-version=udp.sdk-version
      --address="http://sensor.local:9000"
      --word-size=4
      --discoveries={"mdns"}

  discovery := CompositeDiscovery --backends=[
    FakeDiscovery "broken" [] --fails,
    FakeDiscovery "udp" [udp],
    FakeDiscovery "mdns" [mdns],
  ]
  devices := discovery.scan --timeout=(Duration --ms=1)
  expect-equals 1 devices.size
  device/Device := devices.first
  expect-equals "http://sensor.local:9000" device.address
  expect (device.discoveries.contains "udp")
  expect (device.discoveries.contains "mdns")
