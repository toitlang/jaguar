# jag Host Debugger — Design

**Date:** 2026-06-27
**Status:** Approved design, ready for implementation planning
**Repo:** toitlang/jaguar (branch `feature/jag-host-debugger`)

## Goal

Add an interactive bytecode debugger to `jag` for programs run on the host VM
(`jag debug -d host <file>`), built on the Toit VM's existing `--debug` mode and
`dbg:` line protocol. Give operators a `gdb`-like REPL (and a scripted mode for
tests/CI) to set breakpoints by method name, step, and inspect locals — replacing
the throwaway Python driver with a native, first-class jag command.

The host path is the fast experimentation loop the user asked for: `jag run -d host`
already runs the Toit VM locally, so debugging there needs no firmware and no new VM
primitives.

## Context

The Toit VM (branch `feature/host-debugger` in the `toit` repo) already has:
- A `--debug` CLI flag / `OEVM_DEBUG`/`TOIT_DEBUG` env activation.
- A `dbg:` line protocol over stdin/stdout: requests `dbg:methods`, `dbg:break <id> <off>`,
  `dbg:clear <id> <off>`, `dbg:continue`, `dbg:inspect [frame]`, `dbg:step`, `dbg:over`,
  `dbg:out`; responses `dbg:ready`, `dbg:ok <verb>`, `dbg:paused break|step <id> <off>`,
  `dbg:stack off=<n> r0=<v> …`, `dbg:error <msg>`.
- The VM emits **numeric** method ids only. Method-name resolution is done **offline**
  from the snapshot (matches `jag decode`), proven by the Python driver's `build_name_map`
  via `toit tool snapshot bytecodes <snap>`.

A Python proof-of-concept (`tools/debug/` in the toit repo) validated the full protocol,
the held-open-FIFO transport, and offline name resolution end to end against a `count_to`
target. This design ports that capability into jag as native Go.

In jaguar today, `jag run -d host <file>` is caught at `run.go:124` (a string
short-circuit, before any `Device` object exists) and dispatched to `runOnHost`
(`run.go:183`), which execs `toit run -- <args>` (`util.go:156` `ToitRun`) with
stdin/stdout/stderr **inherited** — jag has no visibility into the child's streams.

## Scope

**In scope (this design):**
- A new `jag debug` subcommand targeting `-d host` only.
- Interactive `dbg>` REPL **and** a non-interactive `--script` mode.
- gdb-style command aliases.
- Offline method-name resolution from the snapshot.
- A transport-agnostic relay core behind a `Channel` interface, with host stdio pipes
  as the only concrete transport.

**Deferred, but explicitly architected for (NOT built now):**
- Device debugging (`jag debug -d <esp32>`). Becomes a future `Channel` implementation
  (HTTP or UART) plus the in-image debug supervisor and new VM debug primitives. See
  "Device seam". `-d` with anything other than `host` returns a clean
  "not yet supported" error today.

**Explicitly out of scope (YAGNI):**
- Any firmware / device-side Toit / new VM C++ primitives.
- Debug Adapter Protocol (DAP) / editor integration.
- Conditional breakpoints, watchpoints, expression evaluation beyond `inspect`.
- Class-name resolution for non-Smi inspected values (VM emits `<obj>`; left as-is).
- Debugging a running `.toit` source directly (we always compile to a snapshot first;
  see "Key behaviors").

## Command surface

```
jag debug [-d host] <file.toit> [--script <cmds.txt>]
```

- **Subcommand, not a `run` flag.** Mirrors how every machine-targeting jag verb takes
  `-d`, so the same command extends to `jag debug -d <device>` when a device `Channel`
  exists. Keeps `run`'s logic untouched.
- **`-d` default `host`.** Any other value → error:
  `device debugging is not yet supported (only -d host)`.
- **Interactive by default:** a `dbg>` REPL.
- **`--script <file>`:** read newline-delimited commands, feed the same relay, print the
  transcript, exit. For CI / integration tests.
- Registered via the standard cobra pattern: `DebugCmd()` in `commands/debug.go`, added to
  `cmd.AddCommand(...)` in `jag.go`.

### Command vocabulary & gdb-style aliases

Full verb names are always accepted; aliases are sugar. jag translates each to the wire
`dbg:` verb (the alias scheme is independent of the protocol).

| Alias | Verb | Wire | Notes |
|-------|------|------|-------|
| `b <name\|id> [off]` | break | `dbg:break <id> <off>` | name resolved locally; `off` default 0 |
| `d <name\|id> [off]` | clear (delete) | `dbg:clear <id> <off>` | |
| `c` | continue | `dbg:continue` | |
| `s` | step (into) | `dbg:step` | |
| `n` | over (next) | `dbg:over` | |
| `f` / `fin` | out (finish) | `dbg:out` | |
| `i [frame]` | inspect | `dbg:inspect [frame]` | default frame 0 |
| `m` | methods | `dbg:methods` | |

Plus REPL-local meta-commands (not wire verbs): `help`, `quit`/`q` (detach + exit).

## Architecture

A new transport-agnostic package `cmd/jag/dbg/`, plus thin wiring in `cmd/jag/commands/`.
Five focused, independently testable units.

### `cmd/jag/dbg/` (core — no `os/exec`, no pipes, no cobra)

1. **`Channel` (the device seam)** — the relay is written entirely against this interface;
   it never touches pipes or sockets directly.
   ```go
   type Channel interface {
       Send(cmd string) error   // write one dbg: request line to the target
       Lines() <-chan string    // every line the target emits (dbg: responses + app stdout)
       Close() error
   }
   ```

2. **`protocol.go`** — pure parse/format of `dbg:` lines (port of the Python `parse_line`):
   `ParseLine(s string) Event` where `Event{Kind, Mode, ID, Off, Regs, Verb, Msg, Text}`.
   No I/O. Kinds: `ready`, `paused`, `stack`, `ok`, `error`, `app`, `other`.

3. **`names.go`** — offline name resolution. Given a snapshot path, shells out to
   `toit tool snapshot bytecodes <snap>` and builds `name ↔ entry_bci`. Combined with the
   VM's numeric registry (`dbg:methods` → `{id:(entry_bci,arity)}`, matched on `entry_bci`)
   this yields `name ↔ id`, so `b count-to` → `dbg:break <id> <off>` and a `paused <id>`
   event pretty-prints as `paused in count-to`.

4. **`session.go`** — the relay engine. Owns a `Channel` + the name map. Reads `Lines()`,
   routes `dbg:`-prefixed lines to the protocol handler (pretty-printed) and forwards every
   other line to `os.Stdout` (the debugged program's own output). Exposes `Do(input string)`
   used by both REPL and script; this is where alias→verb translation and name→id resolution
   happen. Waits for `dbg:ready` before the first prompt.

### `cmd/jag/commands/` (wiring)

5. **`stdioChannel`** (host `Channel` impl) — wraps the child VM's `StdinPipe()` /
   `StdoutPipe()`: `Send` writes a line to stdin; `Lines()` scans stdout into the channel.
   The only concrete transport in this design.

6. **`debug.go`** — `DebugCmd()` cobra command: parse `-d`/`--script`; reject non-host;
   compile `<file>` → snapshot (reuse `sdk.Compile`); build the name map; spawn
   `toit run --debug <snap>` via a **new `sdk.ToitRunDebug` helper** (the existing `ToitRun`
   hardcodes a `--` separator that would hide `--debug` from the VM and pass it to the
   program instead); construct `stdioChannel`; hand it to a `dbg.Session`; run REPL
   (default) or feed `--script` lines.

**Why this boundary:** units 1–4 have zero dependency on process spawning, pipes, or cobra.
The relay is exercised entirely through `Channel`, so (a) it unit-tests against a fake
in-memory `Channel` with no VM, and (b) the device path later is a *new file*
(`httpChannel`/`uartChannel`), not a rewrite.

## Data flow

`jag debug -d host count_to.toit`:

```
1. Compile → snapshot   sdk.Compile(file) → <cache>/<uuid>.snapshot
2. Name map             toit tool snapshot bytecodes <snap> → name↔entry_bci
3. Launch               toit run --debug <snap>   [stdin/stdout = pipes; stderr inherited]
4. dbg:ready            session waits for it → prints "ready" → shows dbg> prompt
5. b count-to           resolve name→id → Send "dbg:break <id> 0" → "dbg:ok break"
6. c / n / s / f / i    translate alias → dbg: verb → Channel.Send
7. dbg:paused …         pretty-print "paused in count-to at off N"
   dbg:stack …          "i" output, regs labeled (r0=, r1=, …)
   app stdout (e.g.      forwarded verbatim to the terminal
   "result=10")
8. program exits        session ends; jag exits with the VM's exit code
```

## Key behaviors

- **Debug a snapshot, not the raw `.toit`.** `jag debug` always compiles to a snapshot
  first. A snapshot means only the target app + system run, giving stable program-relative
  bci and clean method ids — the invariant the VM debugger was built around. Running
  `toit run file.toit --debug` would invoke the compiler in-process and interleave programs,
  which breakpoints must not key against. The snapshot also lands in jag's existing cache
  (same as `run`/`decode`).

- **Output splitting.** The VM interleaves `dbg:` responses with the program's own prints on
  one stdout stream. The session splits on the `dbg:` prefix: protocol lines are
  consumed/pretty-printed; everything else streams live to the terminal. (Same rule porta
  and the Python driver use.)

## Error handling

| Situation | Behavior |
|-----------|----------|
| Unknown method name in `break`/`clear` | `error: no method '<name>'` — local, before sending |
| VM returns `dbg:error <msg>` | surfaced as `error: <msg>` |
| `-d` ≠ `host` | `device debugging is not yet supported (only -d host)` |
| toit binary lacks `--debug` (incompatible SDK) | detected at launch; clear message pointing at the SDK build requirement |
| Compile failure | the compiler's diagnostics, non-zero exit (no session started) |
| Malformed REPL input | `error: <usage hint>`; session continues |

## Testing strategy

- **Unit (no VM):**
  - `protocol.go` — table-driven `ParseLine` cases for every `Kind` (ported from the Python
    parser tests).
  - `names.go` — name↔entry_bci mapping from a captured `toit tool snapshot bytecodes` fixture;
    assert `count-to`/`main` resolve to their known entry bcis.
  - `session.go` — drive the relay against a **fake in-memory `Channel`**: feed scripted VM
    lines, assert alias→verb translation, name→id resolution, output splitting (app lines
    forwarded, `dbg:` lines consumed), and pretty-printed pause/stack rendering. No process.

- **Integration (real VM, gated on a debug-capable SDK):**
  - `jag debug --script` against a small `count_to.toit` target **shipped in jag's own
    `testdata/`** (a self-contained copy, no cross-repo path dependency):
    `b count-to` → `c`/`i` loop → assert a transcript line `paused in count-to` (proves name
    resolution end to end) and the program's `result=10`, exit 0. Mirrors the Python smoke
    test, in Go.
  - Skipped with a clear message when no debug-capable `toit` is discoverable.

## Device seam (future work — not built)

The jaguar device supervisor (`jaguar.toit`) is a viable future host for an in-image debug
supervisor: it spawns each container as an isolated process and already keeps a live
`Container` handle (`started-containers_[image]`), and has two bidirectional-capable
transports (HTTP :9000, UART). What's missing is entirely on the VM/firmware side and is the
deferred **Task 7** from the toit-repo debugger plan:

- The C++ `Debugger` is not built/activated on FreeRTOS (it's hardwired to a POSIX
  controller thread on `STDIN_FILENO`).
- The `Container` handle exposes only `stop`/`wait`/`on-stopped` — no pause, set-breakpoint,
  inspect, resume, list-methods, or `on-paused` notification.
- New C++ VM primitives (`debug.pause/break/inspect/resume/methods`) operating cross-process
  on a parked container, plus an `on-paused` callback, would be required.

When that exists, the jag side adds one new `Channel` implementation (e.g. `httpChannel`
against new `/debug/*` endpoints, or a UART channel) and relaxes the `-d host`-only guard.
The relay engine, protocol parser, name resolution, REPL, and alias scheme are unchanged.

## File structure

New / modified in `toitlang/jaguar`:

- `cmd/jag/dbg/protocol.go` — **new**: `dbg:` line parse/format, `Event`.
- `cmd/jag/dbg/names.go` — **new**: offline name↔bci↔id resolution from a snapshot.
- `cmd/jag/dbg/session.go` — **new**: relay engine, alias translation, output splitting.
- `cmd/jag/dbg/channel.go` — **new**: the `Channel` interface (+ a test fake).
- `cmd/jag/dbg/*_test.go` — **new**: unit tests for the above.
- `cmd/jag/commands/debug.go` — **new**: `DebugCmd()`, `stdioChannel`, compile→launch wiring,
  REPL + `--script`.
- `cmd/jag/commands/util.go` — **modified**: add `ToitRunDebug` helper.
- `cmd/jag/commands/jag.go` — **modified**: register `DebugCmd()`.
- `cmd/jag/commands/debug_integration_test.go` — **new**: gated end-to-end smoke test.
- `cmd/jag/commands/testdata/count_to.toit` — **new**: self-contained target fixture for the
  integration test.

## Assumptions & prerequisites

- Requires a `toit` SDK built with the debugger (the `feature/host-debugger` branch, or once
  merged, any SDK). For dev, jag discovers it via `JAG_TOIT_REPO_PATH` →
  `$JAG_TOIT_REPO_PATH/build/host/sdk` (`directory.go:165`), which bypasses the version check.
- `toit run --debug <snapshot>` is the launch form; `--debug` must reach the VM (not be passed
  to the program), hence the `ToitRunDebug` helper rather than the `--`-separated `ToitRun`.
- Name resolution shells out to `toit tool snapshot bytecodes <snap>` — the same tool the
  Python PoC used and that the existing `jag decode` snapshot machinery already relies on.
