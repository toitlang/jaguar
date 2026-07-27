// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

/**
Build metadata shared by the host CLI and its tests.

The release preparation script updates these constants before producing
  binaries. Keeping them in Toit source makes cross-compilation reproducible.
*/
JAG-VERSION ::= "v1.69.0"
SDK-VERSION ::= "v2.0.0-alpha.196"
BUILD-DATE ::= "unknown"
IS-RELEASE ::= false
