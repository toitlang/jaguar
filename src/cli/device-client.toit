// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json
import encoding.ubjson
import crypto.crc
import http
import io
import net

import .build-info
import .device

HEADER-DEVICE-ID          ::= "X-Jaguar-Device-ID"
HEADER-SDK-VERSION        ::= "X-Jaguar-SDK-Version"
HEADER-CONTAINER-NAME     ::= "X-Jaguar-Container-Name"
HEADER-CONTAINER-TIMEOUT  ::= "X-Jaguar-Container-Timeout"
HEADER-CONTAINER-INTERVAL ::= "X-Jaguar-Container-Interval"
HEADER-WIFI-DISABLED      ::= "X-Jaguar-Wifi-Disabled"
HEADER-CRC32              ::= "X-Jaguar-CRC32"
DEFAULT-HTTP-PORT         ::= 9000

/**
Performs host-to-device HTTP operations.

Keeping transport in this class makes command code declarative and gives tests
  one seam at which to substitute a fake device.
*/
class DeviceClient:
  network/net.Interface
  client/http.Client
  owns-network_/bool

  constructor --network/net.Interface?=null:
    owns-network_ = network == null
    this.network = network or net.open
    client = http.Client this.network

  close -> none:
    client.close
    if owns-network_: network.close

  identify address/string -> Device:
    response := with-timeout --ms=2_000:
      client.get --uri="$(normalized-address_ address)/identify"
    ensure-success_ response
    payload := response.body.read-all
    device := Device.from-udp payload
    if not device: throw "Invalid identity response from '$address'"
    device.address = normalized-address_ address
    device.discoveries.clear
    device.discoveries.add "http"
    return device

  ping device/Device -> Duration:
    started := Time.monotonic-us
    response := with-timeout --ms=2_000:
      request := client.new-request http.GET
          --uri="$(normalized-address_ device.address)/ping"
      request.headers.set HEADER-DEVICE-ID device.id
      request.send
    ensure-success_ response
    response.drain
    return Duration --us=(Time.monotonic-us - started)

  container-list device/Device -> Map:
    response := send_ device http.GET "/list"
    payload := response.body.read-all
    decoded := ubjson.decode payload
    if decoded is not Map: throw "Invalid container list from '$(device.name)'"
    return decoded

  container-uninstall device/Device name/string -> none:
    response := send_ device http.PUT "/uninstall"
        --headers={HEADER-CONTAINER-NAME: name}
    response.drain

  send-image
      device/Device
      path/string
      image/ByteArray
      --headers/Map={:}
      -> none:
    all-headers := headers.map: | key value | value
    all-headers[HEADER-CRC32] = "$(crc.crc32 image)"
    response := send_ device http.PUT path --body=image --headers=all-headers
    response.drain

  update-firmware device/Device firmware/ByteArray -> none:
    response := send_ device http.PUT "/firmware" --body=firmware
    response.drain

  send_
      device/Device
      method/string
      path/string
      --body/ByteArray?=null
      --headers/Map={:}
      -> http.Response:
    response := with-timeout --ms=30_000:
      request := client.new-request method
          --uri="$(normalized-address_ device.address)$path"
      request.headers.set HEADER-DEVICE-ID device.id
      request.headers.set HEADER-SDK-VERSION SDK-VERSION
      headers.do: | key/string value |
        request.headers.set key "$value"
      if body: request.body = io.Reader body
      request.send
    ensure-success_ response
    return response

  static ensure-success_ response/http.Response -> none:
    if 200 <= response.status-code < 300: return
    body := response.body.read-all.to-string
    throw "Device returned HTTP $(response.status-code): $body"

normalized-address_ address/string -> string:
  if address.starts-with "http://": return address
  if address.starts-with "https://": return address
  if address.contains ":": return "http://$address"
  return "http://$address:$DEFAULT-HTTP-PORT"
