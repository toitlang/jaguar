// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

// Entry types stored as metadata so the host can filter the captured set.
TYPE-PRINT ::= "print"
TYPE-LOG   ::= "log"
TYPE-TRACE ::= "trace"
// A synthetic entry marking that the producing container stopped; its text is
// the exit code. Lets a container-filtered `run -m` know when the program
// finished.
TYPE-EXIT  ::= "exit"

// Approximate overhead (in bytes) per $LogEntry, used to estimate encoded size
// in ubjson.
ENTRY-OVERHEAD_ ::= 16

/**
A single captured print, log or trace entry.

The $text is pre-rendered for display. The $type and $level are kept as
  metadata so the host can do its own filtering.

The $container is the name jaguar started the producing container under: empty
  for an anonymous `/run` program, the install name for an `/install`-ed
  container, and "?" for a gid jaguar did not start (e.g. a system container).
*/
class LogEntry:
  seq/int
  container/string
  type/string
  level/int?
  text/string
  // Approximate encoded byte cost of this entry, summed into the ring buffer
  // budget for eviction.
  size/int

  constructor .seq .container .type .level .text:
    size = ENTRY-OVERHEAD_ + container.size + type.size + text.size

  /**
  Serializes the entry as a fixed-order array [seq, container, type, level,
  text].

  We use an array instead of a map to avoid repeating field names on every
    entry. This roughly halves the encoded list of entries sent to host.
  */
  to-list -> List:
    return [seq, container, type, level, text]
