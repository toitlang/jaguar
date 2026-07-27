// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import fs
import host.file
import host.os
import host.pipe
import system

import .paths

/**
Locates and invokes the Toit SDK used by Jaguar.
*/
class Sdk:
  executable/string := ?

  constructor paths/Paths:
    executable-name := system.platform == system.PLATFORM-WINDOWS
        ? "toit.exe"
        : "toit"
    repo := os.env.get "JAG_TOIT_REPO_PATH"
    if repo:
      candidate := fs.join (fs.join (fs.join (fs.join repo "build") "host") "sdk") "bin" executable-name
      if not file.is-file candidate:
        throw "JAG_TOIT_REPO_PATH does not contain a built Toit SDK: '$candidate'"
      executable = candidate
    else:
      cached := fs.join paths.sdk-directory "bin" executable-name
      executable = file.is-file cached ? cached : executable-name

  run arguments/List --environment/Map?=null -> none:
    process := pipe.fork executable ([executable] + arguments)
        --environment=environment
    process.wait
    if process.exit-signal:
      throw "Toit SDK terminated by signal $(process.exit-signal)"
    if process.exit-code != 0:
      throw "Toit SDK exited with status $(process.exit-code)"

  capture arguments/List --environment/Map?=null -> string:
    return pipe.backticks
        --environment=environment
        ([executable] + arguments)
