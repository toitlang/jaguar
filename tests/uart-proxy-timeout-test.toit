// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import ..src.jaguar show compute-timeout

main:
  proxied := compute-timeout {"jag.wifi": false} --wifi-disabled --proxied
  assert: proxied == null

  direct := compute-timeout {"jag.wifi": false} --wifi-disabled
  assert: direct == (Duration --s=10)

  proxied-explicit := compute-timeout
      {"jag.timeout": 5}
      --no-wifi-disabled
      --proxied
  assert: proxied-explicit == null

  direct-explicit := compute-timeout
      {"jag.timeout": 5, "jag.wifi": false}
      --wifi-disabled
  assert: direct-explicit == (Duration --s=5)
