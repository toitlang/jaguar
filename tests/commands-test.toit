// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import ..src.host.cli.commands
import ..src.host.cli.paths

main:
  paths := Paths
      --user-config-path="/tmp/jaguar-unused-user.yaml"
      --device-config-path="/tmp/jaguar-unused-device.yaml"
      --snapshots-directory="/tmp/jaguar-unused-snapshots"
      --cache-directory="/tmp/jaguar-unused-cache"
  root := create-root-command paths
  // This validates unique options, command completeness, examples, and all
  // inherited option relationships. Completion is added by Command.run.
  root.check --invoked-command="jag"
