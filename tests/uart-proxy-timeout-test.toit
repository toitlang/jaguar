// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import ..src.jaguar show
    compute-timeout
    note-uart-proxy-activity
    schedule-container-timeout
    uart-proxy-is-active

main:
  implicit := compute-timeout {"jag.wifi": false} --wifi-disabled
  assert: implicit == (Duration --s=10)

  explicit := compute-timeout
      {"jag.timeout": 5, "jag.wifi": false}
      --wifi-disabled
  assert: explicit == (Duration --s=5)

  assert: not uart-proxy-is-active
  note-uart-proxy-activity --window=(Duration --ms=100)
  assert: uart-proxy-is-active

  timed-out := false
  cancel := schedule-container-timeout (Duration --ms=10) --callback=::
    timed-out = true
  sleep --ms=50
  assert: not timed-out

  sleep --ms=100
  assert: timed-out
  cancel.call

  timed-out = false
  cancel = schedule-container-timeout (Duration --ms=10) --uart-only --callback=::
    timed-out = true
  sleep --ms=50
  assert: not timed-out
  cancel.call
