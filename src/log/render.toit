// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import io
import log

/**
Renders a log record the same way the SDK's StandardLogService_ does, so that
  the captured text matches the serial output.
*/
render-log level/int message/string names/List? keys/List? values/List? -> string:
  buffer := io.Buffer
  if names and names.size > 0:
    buffer.write "["
    names.size.repeat:
      if it > 0: buffer.write "."
      buffer.write names[it]
    buffer.write "] "
  buffer.write (log.level-name level)
  buffer.write ": "
  buffer.write message
  if keys and keys.size > 0:
    buffer.write " {"
    keys.size.repeat:
      if it > 0: buffer.write ", "
      buffer.write keys[it]
      buffer.write ": "
      buffer.write values[it]
    buffer.write "}"
  return buffer.to-string
