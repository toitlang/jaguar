// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *
import log

import ..src.log.render as render

main:
  test-render-log

// Rendering matches the SDK's StandardLogService_ output, so the captured text
// is identical to what shows up on the serial console: an optional `[a.b]` name
// prefix, the level name, the message, and an optional `{k: v}` tag suffix.
test-render-log:
  expect-equals "INFO: hello"
      render.render-log log.INFO-LEVEL "hello" null null null
  expect-equals "[a.b] WARN: msg"
      render.render-log log.WARN-LEVEL "msg" ["a", "b"] null null
  expect-equals "[a] INFO: m {k: v}"
      render.render-log log.INFO-LEVEL "m" ["a"] ["k"] ["v"]
