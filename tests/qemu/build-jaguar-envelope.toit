// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import host.file

import ...src.cli.firmware
import ...src.cli.paths

main arguments:
  if arguments.size != 3:
    throw "Usage: build-jaguar-envelope.toit <input> <chip> <output>"

  built := build-firmware
      (Paths --cache-directory="build")
      arguments[0]
      --chip=arguments[1]
      --name="qemu-$(arguments[1])"
      --wifi-ssid="Open Wifi"
      --wifi-password=""
  try:
    file.write-contents
        (file.read-contents built.envelope)
        --path=arguments[2]
  finally:
    built.close
