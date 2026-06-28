// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import log
import monitor

import .capture
import .entry

// Default size in bytes of the ring buffer's budget: the sum over all held
// entries of each entry's field contents plus a fixed per-entry overhead (see
// $ENTRY-OVERHEAD_), which approximates the encoded size of the entries.
DEFAULT-BUFFER-SIZE ::= 4096
// Log entries below this level never enter the ring (the capture floor). Print
// output has no level and is always captured.
DEFAULT-MIN-LEVEL ::= log.INFO-LEVEL
// The bounded hold for a `GET /log` long-poll.
DEFAULT-HOLD ::= Duration --ms=500
// The most bytes (summed $LogEntry.size) a single `drain` returns. Caps the peak
// contiguous allocation needed to encode one `GET /log` response, so a large
// $LogBuffer.buffer-size_ can't make a single response exhaust the device heap.
// A client pages through a longer backlog by polling again with the returned
// "next" cursor. A single entry larger than this is still returned on its own,
// so an oversized line can never stall the stream.
MAX-DRAIN-BYTES ::= 1024
// The filter value that selects the anonymous `/run` programs, which are
// captured under the empty container name.
RUN-CONTAINER-SENTINEL ::= "_run_"

/**
The global capture ring buffer and its service providers.

A single chronological ring mirrors the serial console: all monitored
  containers are interleaved in time order. Entries are evicted oldest-first once
  the total size (see $LogEntry.size) exceeds $buffer-size_.

Capture is off by default and has zero overhead until something opts in: a
  `run -m`/`install -m` or the first `GET /log`. Once enabled, the providers and
  ring stay around until the next reboot, which is the only safe lifetime: a
  container binds to whatever provider exists at its first `print`, so we must
  never unregister a provider while a bound container is still alive.
*/
class LogBuffer:
  // Configuration.
  buffer-size_/int := DEFAULT-BUFFER-SIZE
  min-level_/int := DEFAULT-MIN-LEVEL

  // Ring state.
  entries_/Deque ::= Deque  // Of LogEntry, in seq order.
  total-bytes_/int := 0
  // The seq the next appended entry will get. Entry seqs are 1-based and
  // monotonic; 'head' (the highest seq ever assigned) is next-seq_ - 1.
  next-seq_/int := 1

  // Raised whenever an entry is appended, to wake `/log` long-polls early.
  signal_/monitor.Signal ::= monitor.Signal

  enabled_/bool := false
  provider_/CaptureProvider? := null

  // Maps a running container's system gid to the name jaguar started it under
  // (empty for an anonymous `/run` program). Populated as containers start and
  // dropped as they stop, so the capture handlers - which only see a gid - can
  // stamp each entry with its producing container's name. A gid absent from the
  // map (e.g. a system container that predates capture) renders as "?".
  container-names_/Map ::= {:}  // Of gid/int -> name/string.

  /**
  Enables capture.

  Registers the print/log/trace providers (if not already registered) so that
    containers started from now on bind to them, and lets appends accumulate in
    the ring. Idempotent.

  The providers are never unregistered, even across a later $disable: a container
    resolves and caches its print/log service provider on its first use, so
    unregistering while a bound container is still alive would make that
    container's next `print` hit a dead RPC channel and throw into user code.
    Staying registered until reboot is the only safe lifetime - $disable instead
    leaves them installed but turns them into pass-throughs.
  */
  enable -> none:
    if enabled_: return
    enabled_ = true
    if not provider_:
      provider_ = CaptureProvider this
      provider_.install

  /**
  Disables capture and frees the ring.

  Drops all retained entries and stops appending new ones, reclaiming the
    buffer's memory. The providers stay installed (see $enable) and keep writing
    to the UART, so this is equivalent to capture never having been set up apart
    from the still-registered - but now pass-through - providers. Idempotent.
  */
  disable -> none:
    if not enabled_: return
    enabled_ = false
    entries_.clear
    total-bytes_ = 0

  /** Whether capture is currently enabled. */
  enabled -> bool:
    return enabled_

  /** The highest seq ever assigned (0 if nothing has been captured). */
  head_ -> int:
    return next-seq_ - 1

  /** The lowest seq still in the ring (or head_ + 1 if the ring is empty). */
  oldest_ -> int:
    return entries_.is-empty ? next-seq_ : (entries_.first as LogEntry).seq

  /**
  Registers that the container with the given $gid was started under $name.

  $name is null for an anonymous `/run` program and is stored as the empty
    string. The mapping lets the capture handlers, which only see a gid, stamp
    each entry with its producing container's name.
  */
  register-container --gid/int --name/string? -> none:
    container-names_[gid] = name or ""

  /** Drops the gid->name mapping for a stopped container. */
  unregister-container --gid/int -> none:
    container-names_.remove gid

  /** The name registered for $gid, or "?" if jaguar did not start it. */
  name-for-gid_ gid/int -> string:
    return container-names_.get gid --if-absent=: "?"

  append-print message/string --gid/int -> none:
    if not enabled_: return
    append_ TYPE-PRINT null message --gid=gid

  append-log level/int text/string --gid/int -> none:
    if not enabled_: return
    if level < min-level_: return
    append_ TYPE-LOG level text --gid=gid

  append-trace text/string --gid/int -> none:
    if not enabled_: return
    append_ TYPE-TRACE null text --gid=gid

  /**
  Records that the container with the given $gid stopped with exit $code.

  No-op when capture is not enabled (nothing is draining the ring, so there is
    no reason to grow it).
  */
  append-exit --gid/int --code/int -> none:
    if not enabled_: return
    append_ TYPE-EXIT null "$code" --gid=gid

  append_ type/string level/int? text/string --gid/int -> none:
    entry := LogEntry next-seq_++ (name-for-gid_ gid) type level text
    entries_.add entry
    total-bytes_ += entry.size
    evict_
    signal_.raise

  /** Evicts whole oldest entries until the payload fits $buffer-size_. */
  evict_ -> none:
    // Always keep the most recently added entry, even if it alone is larger
    // than the configured budget.
    while total-bytes_ > buffer-size_ and entries_.size > 1:
      removed/LogEntry := entries_.remove-first
      total-bytes_ -= removed.size

  /**
  Drains the ring for the given $cursor.

  Returns at most $MAX-DRAIN-BYTES worth of entries with seq greater than $cursor
    (optionally filtered to the $containers names). Long-polls up to $max-hold for
    a matching entry to appear, waking early as soon as one does.

  The returned map carries:
  - "entries": the (possibly capped) batch, as $LogEntry.to-list arrays.
  - "next": the cursor to pass to the next drain. When the batch was capped it is
    the seq of the last returned entry, so the next drain continues the backlog;
    otherwise the whole ring was scanned and it is $head_, which also skips past
    any trailing non-matching entries.
  - "head": the global high-water seq, for a fresh `run -m` to start past the
    existing buffer (see $head_).
  - "oldest": the lowest seq still in the ring, for the client's drop detection.

  An empty $containers list matches every container; otherwise an entry matches
    when its container name is in the list. The anonymous `/run` programs are
    captured under the empty name; clients select them with the
    $RUN-CONTAINER-SENTINEL placeholder, which is mapped back to "" here before
    matching.

  Lazily enables capture on the first call.
  */
  drain --cursor/int --containers/List=[] --max-hold/Duration=DEFAULT-HOLD -> Map:
    enable
    containers = containers.map: | name | name == RUN-CONTAINER-SENTINEL ? "" : name
    if not has-match_ cursor containers:
      exception := catch:
        with-timeout max-hold:
          signal_.wait: has-match_ cursor containers
      if exception and exception != DEADLINE-EXCEEDED-ERROR: throw exception
    return collect_ cursor containers

  // Whether $entry is past $cursor and passes the $containers filter (an empty
  // filter matches every container).
  matches_ entry/LogEntry cursor/int containers/List -> bool:
    return entry.seq > cursor and (containers.is-empty or containers.contains entry.container)

  has-match_ cursor/int containers/List -> bool:
    entries_.do: | entry/LogEntry |
      if matches_ entry cursor containers: return true
    return false

  collect_ cursor/int containers/List -> Map:
    list := []
    bytes := 0
    // The seq of the last entry we returned; stays at cursor when nothing matches.
    last-included := cursor
    entries_.do: | entry/LogEntry |
      if matches_ entry cursor containers:
        // Stop before exceeding the cap, but always return at least one entry so
        // an oversized line can't stall the stream. Non-local return doubles as a
        // break: resume the next drain from the last entry we returned.
        if (not list.is-empty) and bytes + entry.size > MAX-DRAIN-BYTES:
          return drain-result_ list last-included
        list.add entry.to-list
        bytes += entry.size
        last-included = entry.seq
    // Scanned the whole ring, so jump next past head, skipping any trailing
    // non-matching entries (max guards a cursor already past head).
    return drain-result_ list (max cursor head_)

  drain-result_ entries/List next/int -> Map:
    return {
      "next": next,
      "head": head_,
      "oldest": oldest_,
      "entries": entries,
    }

  /**
  Applies a configuration $config decoded from the wire.

  The map may contain "buffer_size" (int), "min_level" (a level name like
    "INFO"), and "enabled" (bool). Missing fields leave the matching setting
    unchanged. A shrunk "buffer_size" takes effect immediately by evicting
    entries that no longer fit. "enabled": true calls $enable, "enabled": false
    calls $disable. Returns whether capture is enabled afterwards.
  */
  apply-config config/Map -> bool:
    buffer-size/int? := config.get "buffer_size"
    if buffer-size != null: buffer-size_ = buffer-size
    min-level/string? := config.get "min_level"
    if min-level != null: min-level_ = parse-log-level min-level
    evict_
    enabled := config.get "enabled"
    if enabled == true: enable
    else if enabled == false: disable
    return enabled_

/** Parses a log level name (e.g. "INFO") into its numeric level. */
parse-log-level name/string -> int:
  if name == "TRACE": return log.TRACE-LEVEL
  if name == "DEBUG": return log.DEBUG-LEVEL
  if name == "INFO": return log.INFO-LEVEL
  if name == "WARN": return log.WARN-LEVEL
  if name == "ERROR": return log.ERROR-LEVEL
  if name == "FATAL": return log.FATAL-LEVEL
  throw "unknown log level: $name"
