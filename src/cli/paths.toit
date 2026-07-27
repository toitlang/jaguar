// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import fs
import fs.xdg
import host.os

import .build-info

/**
Resolves all persistent Jaguar paths in one place.

The environment variables and directory layout match the Go implementation,
  so existing installations continue to use their current configuration and
  caches.
*/
class Paths:
  user-config-path/string
  device-config-path/string
  snapshots-directory/string
  cache-directory/string

  constructor
      --user-config-path/string?=null
      --device-config-path/string?=null
      --snapshots-directory/string?=null
      --cache-directory/string?=null:
    config-home := xdg.config-home
    state-home := xdg.state-home
    cache-home := xdg.cache-home

    this.user-config-path = user-config-path
        or os.env.get "JAG_USER_CONFIG_PATH"
        or fs.join config-home "jaguar" "config.yaml"
    this.device-config-path = device-config-path
        or os.env.get "JAG_DEVICE_CONFIG_PATH"
        or fs.join config-home "jaguar" "device.yaml"
    this.snapshots-directory = snapshots-directory
        or os.env.get "JAG_SNAPSHOT_CACHE_PATH"
        or fs.join state-home "toit" "snapshots"
    this.cache-directory = cache-directory
        or os.env.get "JAG_CACHE_DIR"
        or fs.join cache-home "jaguar" JAG-VERSION

  sdk-directory -> string:
    return fs.join cache-directory "sdk"

  envelopes-directory -> string:
    return fs.join cache-directory "envelopes"

  assets-directory -> string:
    return fs.join cache-directory "assets"

  partition-tables-directory -> string:
    return fs.join cache-directory "partition-tables"
