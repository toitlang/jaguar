// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.ubjson
import expect
import http
import log
import monitor
import net
import system.api.firmware show FirmwareService
import system.services show ServiceHandler ServiceProvider
import uuid

import ..src.jaguar show Device
import ..src.network show EndpointHttp HEADER-DEVICE-ID

JAGUAR-CONFIG ::= {"test": "foo"}

main:
  // Must be installed before the first use of system.firmware, which caches
  // its service client.
  service := FakeFirmwareService
  service.install
  try:
    with-endpoint: | client/JagClient |
      test-wifi client service
      test-no-wifi client service
      test-no-firmware-config client service
  finally:
    // An installed service keeps the program alive.
    service.uninstall

test-wifi client/JagClient service/FakeFirmwareService:
  service.config = ubjson.encode {
    "wifi": {
      "wifi.ssid": "test-network",
      "wifi.password": "secret",
    },
    "hostname": "test-host",
  }
  expect.expect-structural-equals {
    "firmware": {
      "wifi": {
        "wifi.ssid": "test-network",
        "wifi.password": "secret",
      },
      "hostname": "test-host",
    },
    "jaguar": JAGUAR-CONFIG,
  } client.get-config

test-no-wifi client/JagClient service/FakeFirmwareService:
  service.config = ubjson.encode {"hostname": "test-host"}
  expect.expect-structural-equals {
    "firmware": {"hostname": "test-host"},
    "jaguar": JAGUAR-CONFIG,
  } client.get-config

test-no-firmware-config client/JagClient service/FakeFirmwareService:
  service.config = #[]
  expect.expect-structural-equals {
    "firmware": {:},
    "jaguar": JAGUAR-CONFIG,
  } client.get-config

class FakeFirmwareService extends ServiceProvider implements ServiceHandler:
  config/ByteArray := ubjson.encode {:}

  constructor:
    super "test/firmware" --major=1 --minor=0
    provides FirmwareService.SELECTOR --handler=this

  handle index/int arguments/any --gid/int --client/int -> any:
    if index == FirmwareService.CONFIG-UBJSON-INDEX: return config
    unreachable

class JagClient:
  http-client/http.Client
  device/Device

  constructor .http-client .device:

  get-config -> Map:
    response := http-client.get --host="localhost" --port=device.port --path="/config"
        --headers=(http.Headers.from-map {HEADER-DEVICE-ID: "$device.id"})
    expect.expect-equals http.STATUS-OK response.status-code
    return ubjson.decode response.body.read-all

with-endpoint [block]:
  network := net.open
  socket := network.tcp-listen 0
  device := Device
      --id=uuid.Uuid.uuid5 "jaguar" "get-config-test"
      --name="test-device"
      --port=socket.local-address.port
      --chip="esp32"
      --config=JAGUAR-CONFIG
  endpoint := EndpointHttp (log.default.with-level log.WARN-LEVEL)
  // A background task doesn't keep the program alive. Stopping the server
  // would make it throw, so it is left running until the program exits.
  task --background::
    endpoint.serve-incoming-requests socket device "" monitor.Mutex
  client := http.Client network
  try:
    block.call (JagClient client device)
  finally:
    client.close
