// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import net
import net.modules.dns

import mdns.client show LocalMdnsClient

import .device
import .device-client

IDENTIFY-PORT ::= 1990
MDNS-SERVICE-TYPE ::= "_jaguar._tcp"

/**
A source of Jaguar device identities.
*/
interface Discovery:
  name -> string
  scan --timeout/Duration -> List

/**
Discovers the legacy periodic UDP identity broadcasts.
*/
class UdpDiscovery implements Discovery:
  port/int

  constructor --.port=IDENTIFY-PORT:

  name -> string:
    return "udp"

  scan --timeout/Duration -> List:
    network := net.open
    socket := network.udp-open --port=port
    found := {:}
    try:
      catch:
        with-timeout timeout:
          while true:
            datagram := socket.receive
            device := Device.from-udp datagram.data
            if device: found[device.id] = device
    finally:
      socket.close
      network.close
    return found.values

/**
Discovers DNS-SD services and validates them through the identify endpoint.
*/
class MdnsDiscovery implements Discovery:
  name -> string:
    return "mdns"

  scan --timeout/Duration -> List:
    mdns/LocalMdnsClient? := null
    device-client/DeviceClient? := null
    network/net.Interface? := null
    result := []
    try:
      network = net.open
      mdns = LocalMdnsClient
      device-client = DeviceClient --network=network
      instances := mdns.browse MDNS-SERVICE-TYPE
          --network=network
          --timeout=timeout
      instances.do: | instance/string |
        catch:
          records := mdns.dns-lookup instance
              --record-types={dns.RECORD-SRV}
              --network=network
              --timeout=timeout
          records.do: | record |
            if record is dns.SrvResource:
              ips := mdns.dns-lookup record.value
                  --record-types={dns.RECORD-A}
                  --network=network
                  --timeout=timeout
              if not ips.is-empty:
                address := "http://$(ips.first):$(record.port)"
                device := device-client.identify address
                device.discoveries.clear
                device.discoveries.add name
                result.add device
    finally:
      if device-client: device-client.close
      if mdns: mdns.close
      if network: network.close
    return result

/**
Combines discovery backends and merges duplicate device IDs.
*/
class CompositeDiscovery implements Discovery:
  backends/List

  constructor --.backends=[UdpDiscovery, MdnsDiscovery]:

  name -> string:
    return "all"

  scan --timeout/Duration -> List:
    calls := backends.map: | backend/Discovery |
      :: safe-scan_ backend timeout
    results := Task.group calls
    found := {:}
    results.values.do: | devices/List |
      devices.do: | device/Device |
        existing := found.get device.id --if-absent=: null
        if existing:
          existing.merge device
        else:
          found[device.id] = device
    return found.values

safe-scan_ backend/Discovery timeout/Duration -> List:
  devices := []
  catch:
    devices = backend.scan --timeout=timeout
  return devices
