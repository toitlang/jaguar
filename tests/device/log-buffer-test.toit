// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import expect show *
import log
import monitor

import ..src.log.buffer show LogBuffer RUN-CONTAINER-SENTINEL MAX-DRAIN-BYTES
import ..src.log.entry

// Field positions within a drained entry array (see LogEntry.to-list).
ENTRY-IDX-SEQ       ::= 0
ENTRY-IDX-CONTAINER ::= 1
ENTRY-IDX-TYPE      ::= 2
ENTRY-IDX-LEVEL     ::= 3
ENTRY-IDX-TEXT      ::= 4

main:
  test-empty
  test-append-noop-when-disabled
  test-append
  test-min-level-floor
  test-cursor-filter
  test-container-filter
  test-container-registry
  test-eviction
  test-eviction-keeps-newest
  test-configure-shrink
  test-has-match
  test-drain
  test-collect-next
  test-collect-cap
  test-collect-cap-oversized

// A fresh ring: nothing captured, head at 0 and oldest one past it (so a client
// polling from cursor 0 sees no drops).
test-empty:
  buffer := LogBuffer
  expect-equals 0 buffer.head_
  expect-equals 1 buffer.oldest_
  result := buffer.collect_ 0 []
  expect-equals 0 result["head"]
  expect-equals 1 result["oldest"]
  expect-equals 0 result["entries"].size

// Appends are only recorded when capture is enabled - otherwise nothing is
// draining the ring and it should not grow. Covers all four append- entry points.
test-append-noop-when-disabled:
  buffer := LogBuffer  // Not enabled.
  buffer.append-print "p" --gid=1
  buffer.append-log log.ERROR-LEVEL "l" --gid=1
  buffer.append-trace "jag decode T" --gid=1
  buffer.append-exit --gid=1 --code=0
  expect-equals 0 buffer.head_
  expect-equals 0 (buffer.collect_ 0 [])["entries"].size

// Appends assign monotonic 1-based seqs and carry the right metadata.
test-append:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.register-container --gid=1 --name="one"
  buffer.register-container --gid=2 --name=""
  buffer.register-container --gid=3 --name="three"
  buffer.append-print "a" --gid=1
  buffer.append-log log.WARN-LEVEL "WARN: b" --gid=2
  buffer.append-trace "jag decode XYZ" --gid=3

  expect-equals 3 buffer.head_
  expect-equals 1 buffer.oldest_

  entries := (buffer.collect_ 0 [])["entries"]
  expect-equals 3 entries.size
  expect-structural-equals
    [1, "one", TYPE-PRINT, null, "a"]
    entries[0]
  expect-structural-equals
    [2, "", TYPE-LOG, log.WARN-LEVEL, "WARN: b"]
    entries[1]
  expect-structural-equals
    [3, "three", TYPE-TRACE, null, "jag decode XYZ"]
    entries[2]

// Log entries below the floor never enter the ring and never consume a seq;
// prints have no level and are always captured.
test-min-level-floor:
  buffer := LogBuffer  // Default floor is INFO.
  buffer.enabled_ = true
  buffer.append-log log.DEBUG-LEVEL "dropped" --gid=1
  buffer.append-log log.INFO-LEVEL "kept-info" --gid=1
  buffer.append-log log.ERROR-LEVEL "kept-error" --gid=1
  buffer.append-print "kept-print" --gid=1

  // Only three of the four made it in, and seqs stay contiguous.
  expect-equals 3 buffer.head_
  entries := (buffer.collect_ 0 [])["entries"]
  expect-equals 3 entries.size
  expect-equals "kept-info" entries[0][ENTRY-IDX-TEXT]
  expect-equals "kept-error" entries[1][ENTRY-IDX-TEXT]
  expect-equals "kept-print" entries[2][ENTRY-IDX-TEXT]

// Draining returns only entries with seq greater than the cursor.
test-cursor-filter:
  buffer := LogBuffer
  buffer.enabled_ = true
  3.repeat: buffer.append-print "m$it" --gid=1

  from-start := (buffer.collect_ 0 [])["entries"]
  expect-equals 3 from-start.size

  tail := (buffer.collect_ 1 [])["entries"]
  expect-equals 2 tail.size
  expect-equals 2 tail[0][ENTRY-IDX-SEQ]
  expect-equals 3 tail[1][ENTRY-IDX-SEQ]

  expect-equals 0 (buffer.collect_ 3 [])["entries"].size

// Draining can be narrowed to a set of producing container names. The empty
// name (anonymous `/run` programs) is a value like any other, and an empty list
// means no filter.
test-container-filter:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.register-container --gid=1 --name="app"
  buffer.register-container --gid=2 --name=null  // Anonymous `/run` -> "".
  buffer.append-print "x" --gid=1
  buffer.append-print "y" --gid=2
  buffer.append-print "z" --gid=1

  all := (buffer.collect_ 0 [])["entries"]  // Empty list: no filter.
  expect-equals 3 all.size

  mine := (buffer.collect_ 0 ["app"])["entries"]
  expect-equals 2 mine.size
  expect-equals "x" mine[0][ENTRY-IDX-TEXT]
  expect-equals "z" mine[1][ENTRY-IDX-TEXT]

  anonymous := (buffer.collect_ 0 [""])["entries"]
  expect-equals 1 anonymous.size
  expect-equals "y" anonymous[0][ENTRY-IDX-TEXT]

  // Several names at once, including the anonymous empty name.
  both := (buffer.collect_ 0 ["app", ""])["entries"]
  expect-equals 3 both.size

  expect-equals 0 (buffer.collect_ 0 ["other"])["entries"].size

// The gid->name registry resolves a gid to its registered name, falls back to
// "?" for an unknown gid, and stores a null name as the empty string.
test-container-registry:
  buffer := LogBuffer
  expect-equals "?" (buffer.name-for-gid_ 1)

  buffer.register-container --gid=1 --name="app"
  buffer.register-container --gid=2 --name=null  // Anonymous `/run`.
  expect-equals "app" (buffer.name-for-gid_ 1)
  expect-equals "" (buffer.name-for-gid_ 2)

  buffer.unregister-container --gid=1
  expect-equals "?" (buffer.name-for-gid_ 1)
  expect-equals "" (buffer.name-for-gid_ 2)

// The ring evicts whole oldest entries once the payload exceeds the budget,
// while head keeps climbing and oldest tracks the lowest surviving seq.
test-eviction:
  buffer := LogBuffer
  buffer.enabled_ = true
  // Each entry's charged size now counts every field plus a fixed overhead, so
  // derive the budget from a real entry instead of hard-coding bytes. All three
  // entries are equal size (same container, type and 4-char text).
  buffer.append-print "aaaa" --gid=1  // seq 1
  entry-size := buffer.total-bytes_
  buffer.apply-config {"buffer_size": 2 * entry-size}  // holds exactly two
  buffer.append-print "bbbb" --gid=1  // seq 2, two entries fit
  buffer.append-print "cccc" --gid=1  // seq 3 -> evict seq 1 -> two left

  expect-equals 3 buffer.head_
  expect-equals 2 buffer.oldest_
  entries := (buffer.collect_ 0 [])["entries"]
  expect-equals 2 entries.size
  expect-equals 2 entries[0][ENTRY-IDX-SEQ]
  expect-equals 3 entries[1][ENTRY-IDX-SEQ]

// The most recently appended entry is always retained, even when it alone
// exceeds the budget.
test-eviction-keeps-newest:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.apply-config {"buffer_size": 1}
  buffer.append-print "aaaaaaaa" --gid=1  // 8 bytes > budget, but it is the newest
  expect-equals 1 (buffer.collect_ 0 [])["entries"].size

  buffer.append-print "bbbbbbbb" --gid=1
  entries := (buffer.collect_ 0 [])["entries"]
  expect-equals 1 entries.size
  expect-equals "bbbbbbbb" entries[0][ENTRY-IDX-TEXT]
  expect-equals 2 buffer.head_
  expect-equals 2 buffer.oldest_

// Shrinking the buffer evicts immediately to fit the new budget.
test-configure-shrink:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.apply-config {"buffer_size": 1_000_000}  // effectively unbounded
  4.repeat: buffer.append-print "1234567890" --gid=1  // four equal entries, all fit
  expect-equals 4 (buffer.collect_ 0 [])["entries"].size

  entry-size := buffer.total-bytes_ / 4
  buffer.apply-config {"buffer_size": 2 * entry-size}  // keep only the last two
  entries := (buffer.collect_ 0 [])["entries"]
  expect-equals 2 entries.size
  expect-equals 3 entries[0][ENTRY-IDX-SEQ]
  expect-equals 4 entries[1][ENTRY-IDX-SEQ]

// The long-poll wake predicate honors both the cursor and the container filter.
test-has-match:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.register-container --gid=1 --name="app"
  buffer.append-print "a" --gid=1
  expect (buffer.has-match_ 0 [])
  expect-not (buffer.has-match_ 1 [])  // cursor at head: nothing newer
  expect (buffer.has-match_ 0 ["app"])
  expect-not (buffer.has-match_ 0 ["other"])

test-drain:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.register-container --gid=1 --name="app"
  buffer.register-container --gid=2 --name=null  // Anonymous `/run` -> "".
  buffer.append-print "one" --gid=1
  // When a matching entry already exists, drain returns it without blocking.
  // The timeout guards against an accidental long-poll: it would fire only if
  // drain blocked on the empty-ring path.
  result := with-timeout (Duration --ms=100): buffer.drain --cursor=0
  entries := result["entries"]
  expect-equals 1 entries.size
  expect-equals "one" entries[0][ENTRY-IDX-TEXT]
  cursor := result["head"]
  expect-equals 1 cursor

  // With no new entries, drain long-polls and wakes as soon as one is appended
  // from another task. It must stay blocked until the append and then return
  // the new entry promptly.
  result = wait-for-drain-to-block-then-send
    buffer
    --cursor=cursor
    --run-when-blocked=(: it.append-print "two" --gid=2 )
  entries = result["entries"]
  expect-equals 1 entries.size
  expect-equals "two" entries[0][ENTRY-IDX-TEXT]
  cursor = result["head"]
  expect-equals 2 cursor

  // Add another entry from the "app" container and validate that waiting on
  // anonymous container still blocks, but returns when we add an entry with
  // matching container.
  buffer.append-print "three" --gid=1

  result = wait-for-drain-to-block-then-send
    buffer
    --cursor=cursor
    --containers=[RUN-CONTAINER-SENTINEL]
    --run-when-blocked=(: it.append-print "four" --gid=2 )
  entries = result["entries"]
  expect-equals 1 entries.size
  expect-equals "four" entries[0][ENTRY-IDX-TEXT]
  cursor = result["head"]
  expect-equals 4 cursor

// When the whole (matching) ring fits in one drain, "next" jumps to head - even
// past trailing entries that the container filter excluded - so the next poll
// long-polls for new output instead of re-scanning them.
test-collect-next:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.register-container --gid=1 --name="app"
  buffer.register-container --gid=2 --name="other"
  buffer.append-print "a" --gid=1  // seq 1, app
  buffer.append-print "b" --gid=1  // seq 2, app
  buffer.append-print "c" --gid=2  // seq 3, other (trailing, excluded by the app filter)

  result := buffer.collect_ 0 ["app"]
  expect-equals 2 result["entries"].size
  expect-equals 3 result["head"]
  expect-equals 3 result["next"]  // advanced to head, past the excluded seq 3

  // Draining again from next is caught up: nothing new, next stays at head.
  caught-up := buffer.collect_ result["next"] ["app"]
  expect-equals 0 caught-up["entries"].size
  expect-equals 3 caught-up["next"]

// A drain returns at most MAX-DRAIN-BYTES of entries; "next" is then the last
// returned seq (below head) and a client pages through the rest. Every entry is
// delivered exactly once across the pages.
test-collect-cap:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.apply-config {"buffer_size": 1_000_000}  // no eviction; build a big backlog
  count := 50
  count.repeat: buffer.append-print ("x" * 100) --gid=1  // ~122 bytes each, >> the cap total

  first := buffer.collect_ 0 []
  entries := first["entries"]
  // Capped: only a fraction of the backlog, sized near the cap (~8 of ~122 bytes).
  expect entries.size < count
  expect entries.size > 0
  expect entries.size <= 12
  expect-equals count first["head"]
  // next is the last returned seq, short of head.
  expect-equals entries[entries.size - 1][ENTRY-IDX-SEQ] first["next"]
  expect first["next"] < first["head"]

  // Page through the remaining backlog with next; tally every delivered entry.
  cursor := first["next"]
  total := entries.size
  seen-last := entries[entries.size - 1][ENTRY-IDX-SEQ]
  while cursor < first["head"]:
    page := buffer.collect_ cursor []
    page-entries := page["entries"]
    expect page-entries.size > 0
    expect-equals (seen-last + 1) page-entries[0][ENTRY-IDX-SEQ]  // continues right after the last seq
    total += page-entries.size
    seen-last = page-entries[page-entries.size - 1][ENTRY-IDX-SEQ]
    cursor = page["next"]
  expect-equals count total  // each entry delivered exactly once

// A single entry larger than the cap is still returned on its own, so an
// oversized line never stalls the stream.
test-collect-cap-oversized:
  buffer := LogBuffer
  buffer.enabled_ = true
  buffer.apply-config {"buffer_size": 1_000_000}
  big := "y" * (MAX-DRAIN-BYTES + 500)
  buffer.append-print big --gid=1     // seq 1, alone exceeds the cap
  buffer.append-print "after" --gid=1  // seq 2

  first := buffer.collect_ 0 []
  expect-equals 1 first["entries"].size
  expect-equals big first["entries"][0][ENTRY-IDX-TEXT]
  expect-equals 1 first["next"]

  second := buffer.collect_ first["next"] []
  expect-equals 1 second["entries"].size
  expect-equals "after" second["entries"][0][ENTRY-IDX-TEXT]

/**
Checks that drain with the given $cursor and $containers will block,
  then runs $run-when-blocked, expects drain to finish and returns
  the resulting value.
*/
wait-for-drain-to-block-then-send -> Map
    buffer/LogBuffer
    --cursor/int
    --containers/List=[]
    [--run-when-blocked]:
  latch := monitor.Latch
  returned := false
  task::
    result := buffer.drain --cursor=cursor --containers=containers
    returned = true
    latch.set result
  sleep --ms=10
  expect-not returned
  // Appending a matching entry should wake the blocked drain.
  run-when-blocked.call buffer
  result := with-timeout (Duration --ms=100): latch.get
  return result
