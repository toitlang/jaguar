// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import fs
import encoding.json
import host.directory
import host.file

import .paths
import .sdk

/**
A relocatable image and the UUID of the snapshot from which it was built.
*/
class ProgramImage:
  bytes/ByteArray
  id/string

  constructor .bytes .id:

/**
Compiles a source or consumes a snapshot, caches its debug information, and
  turns it into a device-word-size-specific image.
*/
build-program-image
    paths/Paths
    source/string
    word-size/int
    --assets/string?=null
    --defines/Map={:}
    --optimization-level/int?=null
    -> ProgramImage:
  if not file.is-file source: throw "No such source or snapshot: '$source'"
  temporary := directory.mkdtemp "/tmp/jaguar-image-"
  try:
    sdk := Sdk paths
    snapshot := source
    if not source.ends-with ".snapshot":
      snapshot = fs.join temporary "program.snapshot"
      arguments := ["compile", "--snapshot", "--output", snapshot]
      if optimization-level != null:
        arguments.add "--optimization-level"
        arguments.add "$optimization-level"
      arguments.add source
      sdk.run arguments

    id := (sdk.capture ["tool", "snapshot", "uuid", snapshot]).trim
    if id.is-empty: throw "Snapshot has no UUID: '$snapshot'"
    if not file.is-directory paths.snapshots-directory:
      directory.mkdir --recursive paths.snapshots-directory
    cached-snapshot := fs.join paths.snapshots-directory "$(id).snapshot"
    file.write-contents (file.read-contents snapshot) --path=cached-snapshot

    effective-assets := assets
    if not defines.is-empty:
      if assets and not file.is-file assets:
        throw "No such assets file: '$assets'"
      definitions-path := fs.join temporary "defines.json"
      file.write-contents (json.encode defines) --path=definitions-path
      merged-assets := fs.join temporary "program.assets"
      if assets:
        sdk.run [
          "tool", "assets", "--assets=$assets",
          "add", "--output=$merged-assets",
          "--format=tison", "jag.defines", definitions-path,
        ]
      else:
        sdk.run ["tool", "assets", "--assets=$merged-assets", "create"]
        sdk.run [
          "tool", "assets", "--assets=$merged-assets",
          "add", "--output=$merged-assets",
          "--format=tison", "jag.defines", definitions-path,
        ]
      effective-assets = merged-assets

    image-path := fs.join temporary "program.image"
    arguments := [
      "tool",
      "snapshot-to-image",
      "--format=binary",
      "--output=$image-path",
      word-size == 8 ? "--machine-64-bit" : "--machine-32-bit",
    ]
    if effective-assets:
      arguments.add "--assets=$effective-assets"
    arguments.add snapshot
    sdk.run arguments
    return ProgramImage (file.read-contents image-path) id
  finally:
    directory.rmdir --recursive temporary
