// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.tison
import system.assets

main:
  definitions := assets.decode.get "jag.defines"
      --if-present=: tison.decode it
      --if-absent=: {:}
  print "JAGUAR-DEFINES: $(definitions["integration.value"])"
