// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import cli

import .cli.commands
import .cli.paths

main arguments:
  root := create-root-command Paths
  root.run arguments
