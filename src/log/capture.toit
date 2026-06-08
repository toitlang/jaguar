// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.base64
import system.services
import system.api.print show PrintService
import system.api.log show LogService
import system.api.trace show TraceService

import .buffer
import .render

/**
The service provider that intercepts print/log/trace output.

Each output kind has its own handler object, since the three services share the
  same method index (0) and must not collide. The providers register at strictly
  higher priority than any system default so that newly started containers bind
  to them.

Providers pass the intercepted entries to $LogBuffer. Entries also get printed
  to UART, mimicking what default log and print handlers do.
*/
class CaptureProvider extends services.ServiceProvider:
  static PRIORITY ::= services.ServiceProvider.PRIORITY-PREFERRED-STRONGLY

  constructor buffer/LogBuffer:
    super "toitlang.org/jaguar-capture" --major=1 --minor=0
    provides PrintService.SELECTOR --handler=(PrintHandler_ buffer) --priority=PRIORITY
    provides LogService.SELECTOR --handler=(LogHandler_ buffer) --priority=PRIORITY
    provides TraceService.SELECTOR --handler=(TraceHandler_ buffer) --priority=PRIORITY

class PrintHandler_ implements services.ServiceHandler:
  buffer_/LogBuffer
  constructor .buffer_:

  handle index/int arguments/any --gid/int --client/int -> any:
    if index == PrintService.PRINT-INDEX:
      message/string := arguments
      // Preserve the serial console (even while capture is disabled).
      write-on-stdout_ message true
      buffer_.append-print message --gid=gid
      return null
    unreachable

class LogHandler_ implements services.ServiceHandler:
  buffer_/LogBuffer
  constructor .buffer_:

  handle index/int arguments/any --gid/int --client/int -> any:
    if index == LogService.LOG-INDEX:
      level/int := arguments[0]
      message/string := arguments[1]
      names/List? := arguments[2]
      keys/List? := arguments[3]
      values/List? := arguments[4]
      text := render-log level message names keys values
      // Preserve the serial console (all levels), then capture at the floor.
      // This mirrors the SDK default path: render as StandardLogService_ does,
      // then write to stdout with a trailing newline (as print's fallback does).
      write-on-stdout_ text true
      buffer_.append-log level text --gid=gid
      return null
    unreachable

class TraceHandler_ implements services.ServiceHandler:
  buffer_/LogBuffer
  constructor .buffer_:

  handle index/int arguments/any --gid/int --client/int -> any:
    if index == TraceService.HANDLE-TRACE-INDEX:
      message/ByteArray := arguments
      buffer_.append-trace "jag decode $(base64.encode message)" --gid=gid
      // Return the message unhandled so the system still prints it to the UART.
      return message
    unreachable

/**
Writes $message directly to the UART/stdout primitive, bypassing the print
  service so that our own provider does not recurse.
*/
write-on-stdout_ message/string add-newline/bool -> none:
  #primitive.core.write-on-stdout
