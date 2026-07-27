// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json

/**
The identity advertised by a Jaguar device.
*/
class Device:
  name/string
  id/string
  chip/string
  sdk-version/string
  address/string := ?
  word-size/int
  discoveries/Set

  constructor
      --.name
      --.id
      --.chip
      --.sdk-version
      --.address
      --.word-size
      --discoveries/Set?=null:
    this.discoveries = discoveries or {}

  static from-identity payload/Map --discovery/string -> Device:
    return Device
        --name=(required-string_ payload "name")
        --id=(required-string_ payload "id")
        --chip=(required-string_ payload "chip")
        --sdk-version=(required-string_ payload "sdkVersion")
        --address=(required-string_ payload "address")
        --word-size=(required-int_ payload "wordSize")
        --discoveries={discovery}

  static from-udp data/ByteArray -> Device?:
    decoded/any := null
    exception := catch: decoded = json.decode data
    if exception: return null
    if decoded is not Map: return null
    if (decoded.get "method" --if-absent=: null) != "jaguar.identify": return null
    payload := decoded.get "payload" --if-absent=: null
    if payload is not Map: return null
    return Device.from-identity payload --discovery="udp"

  merge other/Device -> Device:
    if id != other.id: throw "Cannot merge different Jaguar devices"
    discoveries.add-all other.discoveries
    // Prefer mDNS because it is a resolved unicast address rather than a
    // string copied from a broadcast payload.
    if other.discoveries.contains "mdns":
      address = other.address
    return this

  to-map -> Map:
    return {
      "name": name,
      "id": id,
      "chip": chip,
      "sdkVersion": sdk-version,
      "address": address,
      "wordSize": word-size,
      "discovery": discoveries.to-list.sort,
    }

  matches selector/string -> bool:
    if selector == name or selector == id or selector == address: return true
    if not selector.starts-with "http://" and "http://$selector" == address:
      return true
    if not selector.starts-with "https://" and "https://$selector" == address:
      return true
    return false

required-string_ map/Map key/string -> string:
  value := map.get key --if-absent=: null
  if value is not string: throw "Invalid Jaguar identity field '$key'"
  return value

required-int_ map/Map key/string -> int:
  value := map.get key --if-absent=: null
  if value is not int: throw "Invalid Jaguar identity field '$key'"
  return value
