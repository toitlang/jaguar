// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.yaml
import fs
import host.directory
import host.file

/**
A YAML-backed configuration with dot-separated keys.

The representation is deliberately independent of the CLI package's own
  configuration store because Jaguar must preserve its existing YAML files.
*/
class Config:
  path/string
  data/Map

  constructor .path --data/Map?=null:
    this.data = data or load_ path

  static load_ path/string -> Map:
    if not file.is-file path: return {:}
    decoded := yaml.decode (file.read-contents path)
    if decoded == null: return {:}
    if decoded is not Map: throw "Configuration root must be a map: '$path'"
    return decoded

  get key/string --default=null:
    parts := split-key_ key
    current/any := data
    parts.do: | part/string |
      if current is not Map: return default
      if not current.contains part: return default
      current = current[part]
    return current

  set key/string value -> none:
    parts := split-key_ key
    current := data
    for i := 0; i < parts.size - 1; i++:
      part := parts[i]
      child := current.get part --if-absent=: null
      if child is not Map:
        child = {:}
        current[part] = child
      current = child
    current[parts.last] = value

  remove key/string -> bool:
    parts := split-key_ key
    current := data
    for i := 0; i < parts.size - 1; i++:
      child := current.get parts[i] --if-absent=: null
      if child is not Map: return false
      current = child
    if not current.contains parts.last: return false
    current.remove parts.last
    return true

  save -> none:
    parent := fs.dirname path
    if not file.is-directory parent:
      directory.mkdir --recursive parent
    temporary := "$(path).tmp-$(Time.monotonic-us)"
    try:
      file.write-contents (yaml.encode data) --path=temporary --permissions=0x180
      file.rename temporary path
    finally:
      if file.is-file temporary: file.delete temporary

split-key_ key/string -> List:
  if key == "": throw "Configuration key must not be empty"
  parts := key.split "."
  if (parts.any: it == ""):
    throw "Configuration key contains an empty component: '$key'"
  return parts
