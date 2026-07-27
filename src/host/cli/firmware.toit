// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json
import fs
import host.directory
import host.file
import host.os
import uuid show Uuid

import .paths
import .sdk

/**
A configured firmware envelope and the extraction configuration that belongs
  to it.
*/
class BuiltFirmware:
  temporary-directory/string
  envelope/string
  config/string
  id/string
  name/string
  chip/string

  constructor
      --.temporary-directory
      --.envelope
      --.config
      --.id
      --.name
      --.chip:

  close -> none:
    directory.rmdir --recursive temporary-directory

/**
Installs the Jaguar service and device identity into an envelope.
*/
build-firmware
    paths/Paths
    input-envelope/string?
    --chip/string
    --name/string
    --wifi-ssid/string
    --wifi-password/string
    --exclude-jaguar/bool=false
    -> BuiltFirmware:
  sdk := Sdk paths
  source := resolve-envelope paths input-envelope chip
  temporary := directory.mkdtemp "/tmp/jaguar-firmware-"
  id := "$Uuid.random"
  configured := fs.join temporary "configured.envelope"
  final := fs.join temporary "firmware.envelope"
  firmware-config := fs.join temporary "firmware-config.json"

  exception := catch:
    if exclude-jaguar:
      file.write-contents (file.read-contents source) --path=configured
    else:
      snapshot := fs.join paths.assets-directory "jaguar.snapshot"
      if not file.is-file snapshot:
        snapshot = fs.join "build" "assets" "jaguar.snapshot"
      if not file.is-file snapshot:
        throw "Jaguar device snapshot not found; run 'jag setup' or build it"

      image := fs.join temporary "jaguar.image"
      sdk.run [
        "tool", "snapshot-to-image",
        "--machine-32-bit",
        "--format=binary",
        "--output=$image",
        snapshot,
      ]

      device-json := fs.join temporary "device.json"
      file.write-contents
          (json.encode {"id": id, "name": name, "chip": chip})
          --path=device-json
      assets := fs.join temporary "jaguar.assets"
      sdk.run ["tool", "assets", "--assets=$assets", "create"]
      sdk.run [
        "tool", "assets", "--assets=$assets",
        "add", "--format=tison", "config", device-json,
      ]
      sdk.run [
        "tool", "firmware", "--envelope=$source",
        "container", "install",
        "--output=$configured",
        "--assets=$assets",
        "--trigger=boot",
        "--critical",
        "jaguar", image,
      ]

    sdk.run [
      "tool", "firmware", "--envelope=$configured",
      "property", "set", "--output=$final", "uuid", id,
    ]
    file.write-contents
        (json.encode {
          "wifi": {
            "wifi.ssid": wifi-ssid,
            "wifi.password": wifi-password,
          },
        })
        --path=firmware-config
  if exception:
    directory.rmdir --recursive temporary
    throw exception

  return BuiltFirmware
      --temporary-directory=temporary
      --envelope=final
      --config=firmware-config
      --id=id
      --name=name
      --chip=chip

resolve-envelope paths/Paths requested/string? chip/string -> string:
  if requested:
    if not file.is-file requested: throw "Firmware envelope not found: '$requested'"
    return requested

  repo := os.env.get "JAG_TOIT_REPO_PATH"
  if repo:
    path := fs.join (fs.join (fs.join repo "build") chip) "firmware.envelope"
    if file.is-file path: return path

  cached := fs.join paths.envelopes-directory "firmware-$(chip.to-ascii-lower).envelope"
  if not file.is-file cached:
    throw "Firmware envelope not found: '$cached'; run 'jag setup'"
  return cached
