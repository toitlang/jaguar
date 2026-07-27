// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import cli show Command CommandGroup CompletionCandidate CompletionContext Flag Invocation Option OptionEnum OptionInt Ui
import encoding.json
import encoding.yaml
import fs
import host.file
import host.os
import host.pipe
import net
import system
import uart
import uuid show Uuid

import .build-info
import .config
import .device
import .device-client
import .discovery
import .firmware
import .paths
import .program-image
import .sdk

DEFAULT-SCAN-TIMEOUT ::= Duration --ms=600

/**
Creates the complete command tree.
*/
create-root-command paths/Paths -> Command:
  root := Command "jag"
      --help="""
        Fast development for your ESP32.

        Jaguar uses the Toit virtual machine to update and restart applications
          over WiFi, without reflashing the device for every change.
        """
      --options=[
        Flag "analytics" --hidden --default=true
            --help="Report anonymous usage.",
      ]

  root.add create-version-command
  root.add (create-config-command paths)
  root.add (create-scan-command paths)
  root.add (create-ping-command paths)
  root.add (create-compile-command paths)
  root.add (create-decode-command paths)
  root.add (create-setup-command paths)
  root.add (create-simulate-command paths)
  root.add (create-container-command paths)
  root.add (create-run-command paths)
  root.add (create-port-command paths)
  root.add (create-monitor-command paths)
  root.add (create-firmware-command paths)
  root.add (create-flash-command paths)
  return root

create-version-command -> Command:
  return Command "version"
      --help="Print the Jaguar and SDK versions."
      --run=:: | invocation/Invocation |
        info := {
          "version": IS-RELEASE ? JAG-VERSION : "---",
          "sdkVersion": SDK-VERSION,
          "buildDate": BUILD-DATE,
        }
        invocation.cli.ui.emit
            --kind=Ui.RESULT
            --structured=: info
            --text=:
              """
              Version:\t $(info["version"])
              SDK version:\t $(info["sdkVersion"])
              Build date:\t $(info["buildDate"])
              """

create-config-command paths/Paths -> Command:
  command := Command "config"
      --help="Configure the Jaguar command-line tool."

  analytics := Command "analytics"
      --help="Configure anonymous usage and crash reporting."
  analytics.add (toggle-command
      "enable"
      "Enable anonymous reporting."
      paths
      "analytics.disabled"
      false)
  analytics.add (toggle-command
      "disable"
      "Disable anonymous reporting."
      paths
      "analytics.disabled"
      true
      --also-key="up-to-date.disabled")
  command.add analytics

  up-to-date := Command "up-to-date"
      --help="Configure periodic update checks."
  up-to-date.add (toggle-command
      "enable"
      "Enable periodic update checks."
      paths
      "up-to-date.disabled"
      false)
  up-to-date.add (toggle-command
      "disable"
      "Disable periodic update checks."
      paths
      "up-to-date.disabled"
      true)
  command.add up-to-date

  wifi := Command "wifi"
      --help="Configure default WiFi credentials for Jaguar devices."
  wifi.add (Command "set"
      --help="Set the default WiFi network and password."
      --options=[
        Option "wifi-ssid"
            --help="Default WiFi network name."
            --default=(os.env.get "JAG_WIFI_SSID")
            --required=(os.env.get "JAG_WIFI_SSID") == null,
        Option "wifi-password"
            --help="Default WiFi password."
            --default=(os.env.get "JAG_WIFI_PASSWORD")
            --required=(os.env.get "JAG_WIFI_PASSWORD") == null,
      ]
      --run=:: | invocation/Invocation |
        update-config paths: | config/Config |
          config.set "wifi.ssid" invocation["wifi-ssid"]
          config.set "wifi.password" invocation["wifi-password"])
  wifi.add (Command "clear"
      --help="Delete the stored WiFi credentials."
      --run=:: | _ |
        update-config paths: | config/Config |
          config.remove "wifi.ssid"
          config.remove "wifi.password")
  command.add wifi

  cache := Command "cache"
      --help="Configure cache retention."
  cache.add (Command "set-keep-old"
      --help="Keep old cached files."
      --run=:: | _ |
        update-config paths: it.set "cache.keep_old" true)
  cache.add (Command "clear-keep-old"
      --help="Restore removal of old cached files."
      --run=:: | _ |
        update-config paths: it.remove "cache.keep_old")
  command.add cache

  identify := Command "identify"
      --help="Configure device identification."
  identify.add (Command "timeout"
      --help="Get or set the device-identification timeout."
      --options=[
        Flag "clear" --help="Clear the configured timeout.",
      ]
      --rest=[
        Option "duration" --help="A duration such as 1s.",
      ]
      --run=:: | invocation/Invocation |
        config := Config paths.user-config-path
        duration := invocation["duration"]
        if invocation["clear"]:
          if duration: throw "Cannot use --clear with a duration"
          config.remove "identify.timeout"
          config.save
        else if duration:
          parsed := Duration.parse duration
          config.set "identify.timeout" "$parsed"
          config.save
        else:
          print (config.get "identify.timeout" --default="Default: 1s"))
  command.add identify
  return command

toggle-command
    name/string
    help/string
    paths/Paths
    key/string
    value/bool
    --also-key/string?=null
    -> Command:
  return Command name
      --help=help
      --run=:: | _ |
        config := Config paths.user-config-path
        config.set key value
        if also-key: config.set also-key value
        config.save

update-config paths/Paths [block] -> none:
  config := Config paths.user-config-path
  block.call config
  config.save

create-scan-command paths/Paths -> Command:
  return Command "scan"
      --help="Scan for Jaguar devices using UDP and mDNS."
      --options=[
        Flag "list" --short-name="l"
            --help="List all discovered devices.",
        OptionEnum "output" ["short", "json", "yaml"]
            --short-name="o"
            --help="Select the list output format."
            --default="short",
        Option "timeout" --short-name="t"
            --help="How long to scan."
            --default="$DEFAULT-SCAN-TIMEOUT",
        OptionInt "port" --short-name="p"
            --help="Set the UDP discovery port."
            --default=IDENTIFY-PORT,
        OptionEnum "discovery" ["all", "udp", "mdns"]
            --help="Select the discovery protocol."
            --default="all",
      ]
      --rest=[
        Option "device"
            --help="A device name, ID, or address."
            --completion=:: complete-device it,
      ]
      --run=:: | invocation/Invocation | run-scan invocation paths

run-scan invocation/Invocation paths/Paths -> none:
  timeout := Duration.parse invocation["timeout"]
  selector := invocation["device"]
  if invocation["list"] and selector:
    throw "Listing and device selection are exclusive"

  devices := selector and looks-like-address_ selector
      ? [identify-address selector]
      : scan-with invocation["discovery"] timeout invocation["port"]

  if invocation["list"]:
    print-devices devices invocation["output"] invocation
    return

  if selector:
    devices = devices.filter: it.matches selector
  if devices.is-empty:
    throw "Didn't find any Jaguar devices"
  if devices.size > 1:
    throw "Found multiple devices; specify one by name, ID, or address"

  selected/Device := devices.first
  config := Config paths.device-config-path
  config.set "device" selected.to-map
  config.save
  print-device selected invocation["output"] invocation

scan-with protocol/string timeout/Duration port/int -> List:
  discovery/Discovery := protocol == "udp"
      ? UdpDiscovery --port=port
      : protocol == "mdns"
          ? MdnsDiscovery
          : CompositeDiscovery --backends=[UdpDiscovery --port=port, MdnsDiscovery]
  return discovery.scan --timeout=timeout

identify-address address/string -> Device:
  client := DeviceClient
  try:
    return client.identify address
  finally:
    client.close

print-devices devices/List format/string invocation/Invocation -> none:
  maps := devices.map: it.to-map
  if format == "json":
    print (json.stringify maps)
  else if format == "yaml":
    print (yaml.stringify maps)
  else:
    devices.do: print-device it "short" invocation

print-device device/Device format/string invocation/Invocation -> none:
  if format == "json":
    print (json.stringify device.to-map)
  else if format == "yaml":
    print (yaml.stringify device.to-map)
  else:
    invocation.cli.ui.emit --kind=Ui.RESULT
        --structured=: device.to-map
        --text=: "$(device.name)\t$(device.address)\t$(device.id)"

create-ping-command paths/Paths -> Command:
  return Command "ping"
      --help="Ping a Jaguar device."
      --options=[
        Option "device" --short-name="d"
            --help="Use a device with the given name, ID, or address."
            --completion=:: complete-device it,
      ]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        client := DeviceClient
        try:
          elapsed := client.ping device
          invocation.cli.ui.emit --kind=Ui.RESULT
              --structured=: {"device": device.to-map, "latencyUs": elapsed.in-us}
              --text=: "Got pong from $(device.name) in $elapsed"
        finally:
          client.close

resolve-device selector/string? paths/Paths -> Device:
  if selector and looks-like-address_ selector:
    return identify-address selector

  if not selector:
    config := Config paths.device-config-path
    saved := config.get "device"
    if saved is Map:
      device := Device.from-identity saved --discovery="config"
      if saved.contains "discovery":
        device.discoveries.clear
        saved["discovery"].do: device.discoveries.add it
      return device

  devices := (CompositeDiscovery).scan --timeout=DEFAULT-SCAN-TIMEOUT
  matches := devices.filter: it.matches selector
  if matches.size != 1: throw "Could not uniquely resolve Jaguar device '$selector'"
  return matches.first

complete-device context/CompletionContext -> List:
  devices/List := []
  exception := catch:
    devices = (CompositeDiscovery).scan --timeout=(Duration --ms=450)
  if exception: return []
  result := []
  devices.do: | device/Device |
    if device.name.starts-with context.prefix:
      result.add (CompletionCandidate device.name --description=device.address)
    if device.id.starts-with context.prefix:
      result.add (CompletionCandidate device.id --description=device.name)
  return result

looks-like-address_ value/string -> bool:
  if value.starts-with "http://": return true
  if value.starts-with "https://": return true
  candidate := value
  colon := value.index-of ":"
  if colon >= 0: candidate = value[..colon]
  return net.IpAddress.is-valid candidate

create-compile-command paths/Paths -> Command:
  return Command "compile"
      --hidden
      --help="Compile Toit code to a snapshot."
      --options=[
        Option "output" --short-name="o"
            --type="file"
            --help="Set the output snapshot path.",
        OptionInt "optimization-level" --short-name="O"
            --help="Set the optimization level.",
      ]
      --rest=[
        Option "source" --type="file"
            --help="The Toit source file."
            --required,
      ]
      --run=:: | invocation/Invocation |
        source := invocation["source"]
        if not file.is-file source: throw "No such source file: '$source'"
        output := invocation["output"]
        if not output:
          dot := source.last-index-of "."
          if dot < 0: throw "Cannot derive a snapshot name from '$source'"
          output = source[..dot] + ".snapshot"
        arguments := ["compile", "--snapshot", "--output", output]
        optimization := invocation["optimization-level"]
        if optimization != null:
          arguments.add "--optimization-level"
          arguments.add "$optimization"
        arguments.add source
        print "Compiling '$source' to '$output'"
        (Sdk paths).run arguments
        print "Success: Wrote compiled bytecodes to '$output'"

create-decode-command paths/Paths -> Command:
  return Command "decode"
      --help="Decode a system message received from a Jaguar device."
      --options=[
        Flag "force-pretty" --short-name="r"
            --help="Force terminal graphics.",
        Flag "force-plain" --short-name="l"
            --help="Force plain ASCII output.",
        Option "envelope" --type="file"
            --help="Set the firmware envelope for a native backtrace.",
      ]
      --rest=[
        Option "message"
            --help="The encoded system message."
            --required,
      ]
      --run=:: | invocation/Invocation |
        message/string := invocation["message"]
        if message.starts-with "jag decode ": message = message[11..]
        if message.starts-with "Backtrace:":
          throw "Native backtrace decoding needs an SDK stacktrace API"
        arguments := ["decode"]
        if invocation["force-pretty"]: arguments.add "--force-pretty"
        if invocation["force-plain"]: arguments.add "--force-plain"
        arguments.add message
        (Sdk paths).run arguments

create-setup-command paths/Paths -> Command:
  default := Command "setup"
      --help="Set up the Toit SDK and Jaguar assets."
      --options=[
        Flag "check" --short-name="c"
            --help="Check that the local setup is complete.",
        Flag "skip-assets" --short-name="s" --hidden
            --help="Skip the Jaguar assets download.",
        OptionEnum "print-path" ["assets", "sdk"] --hidden
            --help="Print a setup directory.",
        Flag "keep-old"
            --help="Keep caches for older Jaguar versions.",
      ]
      --run=:: | invocation/Invocation |
        print-path := invocation["print-path"]
        if print-path:
          print (print-path == "assets"
              ? paths.assets-directory
              : paths.sdk-directory)
        else if invocation["check"]:
          executable-name := system.platform == system.PLATFORM-WINDOWS
              ? "toit.exe"
              : "toit"
          sdk-executable := fs.join
              (fs.join paths.sdk-directory "bin")
              executable-name
          if not file.is-file sdk-executable:
            throw "Toit SDK is not installed at '$sdk-executable'"
          snapshot := fs.join paths.assets-directory "jaguar.snapshot"
          if not invocation["skip-assets"] and not file.is-file snapshot:
            throw "Jaguar assets are not installed at '$(paths.assets-directory)'"
          print "Jaguar setup is valid."
        else:
          throw """
            SDK downloads need the certificate-roots dependency, whose
              installation is currently blocked by the package-manager
              LOCK_STALE failure; see TOIT_SDK_GAPS.md.
            """

  commands := Command "setup"
  commands.add (Command "sdk"
      --help="Install just the Toit SDK into a directory."
      --rest=[
        Option "directory" --type="directory"
            --help="The SDK destination directory."
            --required,
      ]
      --run=:: | _ |
        throw """
          SDK downloads need the certificate-roots dependency, whose
            installation is currently blocked by the package-manager
            LOCK_STALE failure; see TOIT_SDK_GAPS.md.
          """)
  return CommandGroup "setup"
      --help="Set up the Toit SDK and Jaguar assets."
      --default=default
      --commands=commands

create-simulate-command paths/Paths -> Command:
  return Command "simulate"
      --help="Start a simulated Jaguar device on this machine."
      --options=[
        OptionInt "port" --short-name="p"
            --help="Set the simulator HTTP port."
            --default=0,
        Option "name"
            --help="Set the simulator name.",
      ]
      --run=:: | invocation/Invocation |
        snapshot := fs.join paths.assets-directory "jaguar.snapshot"
        if not file.is-file snapshot:
          // Development builds keep generated assets next to the host binary.
          snapshot = fs.join "build" "assets" "jaguar.snapshot"
        if not file.is-file snapshot:
          throw "Jaguar device snapshot not found; run 'jag setup' or build it"
        id := Uuid.random
        name := invocation["name"] or "simulator-$("$id"[..8])"
        (Sdk paths).run [
          "run",
          snapshot,
          "$(invocation["port"])",
          "$id",
          name,
        ]

create-container-command paths/Paths -> Command:
  command := Command "container"
      --help="Manipulate containers installed on a Jaguar device."

  command.add (Command "list"
      --help="List installed containers."
      --options=[device-option]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        client := DeviceClient
        try:
          containers := client.container-list device
          print "DEVICE\tIMAGE\tNAME"
          containers.do: | image name |
            print "$(device.name)\t$image\t$name"
        finally:
          client.close)

  command.add (Command "uninstall"
      --help="Uninstall a container."
      --options=[device-option]
      --rest=[
        Option "name" --help="The container name." --required,
      ]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        print "Uninstalling container '$(invocation["name"])' on '$(device.name)'"
        client := DeviceClient
        try:
          client.container-uninstall device invocation["name"]
        finally:
          client.close)

  command.add (Command "install"
      --help="Compile and install a container."
      --options=program-options --with-interval
      --rest=[
        Option "name" --help="The container name." --required,
        Option "source" --type="file" --help="The source or snapshot." --required,
      ]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        defines := parse-defines invocation["define"]
        image := build-program-image
            paths
            invocation["source"]
            device.word-size
            --assets=invocation["assets"]
            --defines=defines.assets
            --optimization-level=invocation["optimization-level"]
        headers := defines.headers
        headers[HEADER-CONTAINER-NAME] = invocation["name"]
        interval := invocation["interval"]
        if interval:
          Duration.parse interval
          headers[HEADER-CONTAINER-INTERVAL] = interval
        client := DeviceClient
        try:
          client.send-image device "/install" image.bytes --headers=headers
        finally:
          client.close)
  return command

create-run-command paths/Paths -> Command:
  return Command "run"
      --help="Run Toit code on a Jaguar device or on the host."
      --dash-dash-is-rest
      --options=(program-options + [
        Option "expression" --short-name="s"
            --help="Evaluate an immediate Toit expression.",
      ])
      --rest=[
        Option "source" --type="file"
            --help="The source or snapshot.",
        Option "argument" --multi
            --help="An argument passed to a host program.",
      ]
      --run=:: | invocation/Invocation |
        selector := invocation["device"]
        expression := invocation["expression"]
        if selector == "host":
          if not invocation["define"].is-empty:
            throw "--define is not supported for host runs"
          if expression:
            throw "Immediate expressions need SDK support for 'toit run -s'"
          if not invocation["source"]: throw "No input file provided"
          arguments := ["run"]
          optimization := invocation["optimization-level"]
          if optimization != null:
            arguments.add "--optimization-level=$optimization"
          arguments.add invocation["source"]
          arguments.add-all invocation["argument"]
          (Sdk paths).run arguments
        else:
          if expression:
            throw "--expression/-s is only supported with '--device=host'"
          if not invocation["source"]: throw "No input file provided"
          if not invocation["argument"].is-empty:
            throw "Program arguments are only supported with '--device=host'"
          device := resolve-device selector paths
          defines := parse-defines invocation["define"]
          image := build-program-image
              paths
              invocation["source"]
              device.word-size
              --assets=invocation["assets"]
              --defines=defines.assets
              --optimization-level=invocation["optimization-level"]
          headers := defines.headers
          client := DeviceClient
          try:
            client.send-image device "/run" image.bytes --headers=headers
          finally:
            client.close

device-option -> Option:
  return Option "device" --short-name="d"
      --help="Use a device with the given name, ID, or address."
      --completion=:: complete-device it

program-options --with-interval/bool=false -> List:
  result := [
    device-option,
    Option "define" --short-name="D" --multi
        --help="Define a setting for the program.",
    Option "assets" --type="file"
        --help="Attach an assets file.",
    OptionInt "optimization-level" --short-name="O"
        --help="Set the optimization level."
        --default=1,
  ]
  if with-interval:
    result.add (Option "interval" --help="Set the restart interval.")
  return result

class ParsedDefines:
  headers/Map
  assets/Map

  constructor .headers .assets:

parse-defines definitions/List -> ParsedDefines:
  headers := {:}
  assets := {:}
  definitions.do: | definition/string |
    separator := definition.index-of "="
    key := (separator < 0 ? definition : definition[..separator]).trim
    raw := separator < 0 ? "true" : definition[separator + 1..].trim
    if key == "jag.disabled":
      key = "jag.wifi"
      raw = "false"
    if key == "jag.wifi":
      if raw == "false":
        headers[HEADER-WIFI-DISABLED] = "true"
      else if raw != "true":
        throw "jag.wifi must be true or false"
    else if key == "jag.timeout":
      seconds := int.parse raw --if-error=: null
      if seconds == null:
        duration := Duration.parse raw
        seconds = (duration.in-us + 999_999) / 1_000_000
      headers[HEADER-CONTAINER-TIMEOUT] = "$seconds"
    else if key == "jag.interval":
      Duration.parse raw
      headers[HEADER-CONTAINER-INTERVAL] = raw
    else if key.starts-with "jag.":
      throw "Unsupported Jaguar define: '$key'"
    else:
      value/any := raw
      catch:
        value = json.decode raw.to-byte-array
      assets[key] = value
  return ParsedDefines headers assets

create-port-command paths/Paths -> Command:
  default := Command "port"
      --options=[
        Flag "list" --short-name="l"
            --help="List available serial ports.",
        OptionEnum "output" ["short", "json", "yaml"]
            --short-name="o"
            --help="Select the port-list output format."
            --default="short",
        Flag "all"
            --help="Include ports that do not look like ESP32 devices.",
      ]
      --run=:: | invocation/Invocation |
        if invocation["list"]:
          throw "Portable serial-port enumeration is not exposed by the SDK"
        config := Config paths.device-config-path
        port := config.get "port"
        if not port: throw "Port is not set; use 'jag port set <port>'"
        print port
  commands := Command "port"
  commands.add (Command "set"
      --help="Select a serial port."
      --options=[
        Flag "all"
            --help="Include ports that do not look like ESP32 devices.",
      ]
      --rest=[
        Option "port" --help="The serial device path.",
      ]
      --run=:: | invocation/Invocation |
        if not invocation["port"]:
          throw "Interactive port selection needs portable SDK serial enumeration"
        config := Config paths.device-config-path
        config.set "port" invocation["port"]
        config.save)
  return CommandGroup "port"
      --help="Get or configure the serial port."
      --default=default
      --commands=commands

create-monitor-command paths/Paths -> Command:
  configured := (Config paths.device-config-path).get "port"
  return Command "monitor"
      --help="Monitor the serial output of an ESP32."
      --options=[
        Option "port" --short-name="p"
            --help="Set the serial port."
            --default=configured,
        OptionInt "baud"
            --help="Set the serial baud rate."
            --default=115_200,
        Flag "attach" --short-name="a"
            --help="Attach without rebooting the device.",
        Flag "force-pretty" --short-name="r"
            --help="Force terminal graphics when decoding.",
        Flag "force-plain" --short-name="l"
            --help="Force plain output when decoding.",
        Flag "proxy"
            --help="Proxy the attached device to the local network.",
        Option "envelope" --type="file"
            --help="Set the envelope for native crash decoding.",
      ]
      --run=:: | invocation/Invocation |
        port-path := invocation["port"]
        if not port-path: throw "No serial port configured"
        if invocation["proxy"]:
          throw "UART network proxying needs host socket and cancellation support"
        if invocation["envelope"]:
          throw "Native crash decoding needs an SDK stacktrace API"
        serial := (uart.Port port-path --baud-rate=invocation["baud"]) as uart.HostPort
        try:
          if not invocation["attach"]:
            serial.set-control-flag uart.HostPort.CONTROL-FLAG-DTR false
            serial.set-control-flag uart.HostPort.CONTROL-FLAG-RTS true
            sleep --ms=100
            serial.set-control-flag uart.HostPort.CONTROL-FLAG-RTS false
          print "Starting serial monitor of port '$port-path' ..."
          while data := serial.in.read:
            pipe.stdout.out.write data
        finally:
          serial.close

create-firmware-command paths/Paths -> Command:
  default := Command "firmware"
      --options=[device-option]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        print "Device '$(device.name)' is running Toit SDK $(device.sdk-version)"

  commands := Command "firmware"
  commands.add (Command "extract"
      --help="Build a complete flash image from an envelope."
      --options=(firmware-options + [
        Option "output" --short-name="o" --type="file"
            --help="Write the flash image to this path."
            --required,
        Option "partition-table" --type="file"
            --help="Override the partition table.",
      ])
      --rest=[
        Option "envelope" --type="file"
            --help="The base firmware envelope."
            --required,
      ]
      --run=:: | invocation/Invocation |
        settings := firmware-settings invocation paths
        built := build-firmware
            paths
            invocation["envelope"]
            --chip=settings["chip"]
            --name=settings["name"]
            --wifi-ssid=settings["wifiSsid"]
            --wifi-password=settings["wifiPassword"]
            --exclude-jaguar=invocation["exclude-jaguar"]
        try:
          arguments := [
            "tool", "firmware", "--envelope=$(built.envelope)",
            "extract", "--format=image",
            "--config=$(built.config)",
            "--output=$(invocation["output"])",
          ]
          partition-table := invocation["partition-table"]
          if partition-table: arguments.add "--partitions=$partition-table"
          (Sdk paths).run arguments
        finally:
          built.close)

  commands.add (Command "update"
      --help="Update a Jaguar device over WiFi."
      --options=([device-option] + firmware-options)
      --rest=[
        Option "envelope" --type="file"
            --help="The base firmware envelope.",
      ]
      --run=:: | invocation/Invocation |
        device := resolve-device invocation["device"] paths
        settings := firmware-settings invocation paths --device=device
        built := build-firmware
            paths
            invocation["envelope"]
            --chip=device.chip
            --name=settings["name"]
            --wifi-ssid=settings["wifiSsid"]
            --wifi-password=settings["wifiPassword"]
            --exclude-jaguar=invocation["exclude-jaguar"]
        temporary-binary := fs.join built.temporary-directory "firmware.bin"
        try:
          (Sdk paths).run [
            "tool", "firmware", "--envelope=$(built.envelope)",
            "extract", "--format=binary",
            "--config=$(built.config)",
            "--output=$temporary-binary",
          ]
          client := DeviceClient
          try:
            client.update-firmware device (file.read-contents temporary-binary)
          finally:
            client.close
          updated := Device
              --name=built.name
              --id=built.id
              --chip=built.chip
              --sdk-version=SDK-VERSION
              --address=device.address
              --word-size=device.word-size
              --discoveries=device.discoveries
          config := Config paths.device-config-path
          config.set "device" updated.to-map
          config.save
        finally:
          built.close)

  return CommandGroup "firmware"
      --help="Show or update firmware for a Jaguar device."
      --default=default
      --commands=commands

create-flash-command paths/Paths -> Command:
  configured-port := (Config paths.device-config-path).get "port"
  return Command "flash"
      --help="Flash an ESP32 with Jaguar firmware over serial."
      --options=(firmware-options + [
        Option "port" --short-name="p" --type="file"
            --help="Set the serial port."
            --default=configured-port
            --required=configured-port == null,
        OptionInt "baud"
            --help="Set the flashing baud rate."
            --default=921_600,
        Flag "skip-port-check"
            --help="Accept the port without enumeration.",
        Option "partition-table" --type="file"
            --help="Override the partition table.",
      ])
      --rest=[
        Option "envelope" --type="file"
            --help="The base firmware envelope.",
      ]
      --run=:: | invocation/Invocation |
        if not invocation["skip-port-check"]:
          throw """
            Port validation needs portable SDK serial enumeration; use
              --skip-port-check to accept the configured port explicitly.
            """
        settings := firmware-settings invocation paths
        built := build-firmware
            paths
            invocation["envelope"]
            --chip=settings["chip"]
            --name=settings["name"]
            --wifi-ssid=settings["wifiSsid"]
            --wifi-password=settings["wifiPassword"]
            --exclude-jaguar=invocation["exclude-jaguar"]
        try:
          arguments := [
            "tool", "firmware", "--envelope=$(built.envelope)",
            "flash",
            "--port=$(invocation["port"])",
            "--baud=$(invocation["baud"])",
            "--config=$(built.config)",
          ]
          partition-table := invocation["partition-table"]
          if partition-table: arguments.add "--partitions=$partition-table"
          (Sdk paths).run arguments
        finally:
          built.close

firmware-options -> List:
  return [
    Option "name" --help="Set the Jaguar device name.",
    OptionEnum "chip" ["esp32", "esp32c3", "esp32c6", "esp32s2", "esp32s3"]
        --short-name="c"
        --help="Set the target chip."
        --default="esp32",
    Option "wifi-ssid"
        --help="Set the WiFi network name."
        --default=(os.env.get "JAG_WIFI_SSID"),
    Option "wifi-password"
        --help="Set the WiFi password."
        --default=(os.env.get "JAG_WIFI_PASSWORD"),
    Flag "exclude-jaguar"
        --help="Do not install the Jaguar service.",
  ]

firmware-settings
    invocation/Invocation
    paths/Paths
    --device/Device?=null
    -> Map:
  config := Config paths.user-config-path
  ssid := invocation["wifi-ssid"] or config.get "wifi.ssid"
  password := invocation["wifi-password"]
  if password == null: password = config.get "wifi.password"
  if ssid == null:
    throw "WiFi credentials are not configured; use 'jag config wifi set'"
  if password == null:
    throw "WiFi password is not configured; use 'jag config wifi set'"
  return {
    "chip": device ? device.chip : invocation["chip"],
    "name": invocation["name"] or (device ? device.name : "jaguar"),
    "wifiSsid": ssid,
    "wifiPassword": password,
  }
