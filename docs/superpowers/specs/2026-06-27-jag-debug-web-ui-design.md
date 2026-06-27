# jag Debug Web UI — Design

**Date:** 2026-06-27
**Status:** Approved design, ready for implementation planning
**Repo:** toitlang/jaguar (branch `feature/jag-host-debugger`)
**Builds on:** the host debugger (`docs/superpowers/specs/2026-06-27-jag-host-debugger-design.md`,
implemented as `jag debug -d host`). This spec adds a browser front-end.

## Goal

Add `jag debug --web <file.toit>`: a browser-based view of a host debug session.
The page shows the program's **source with the current line highlighted as you
step**, lets you **set/clear breakpoints by clicking the gutter**, drives
execution with **step / over / out / continue** buttons, and shows a **raw
variables panel** (the current frame's `rN=` values). The debugged program's own
output stays on the launch console.

This is the visual debugger experience without the weight of a full DAP / editor
plugin, and it is a down payment on future device debugging (see "Device
symmetry").

## Context

The host debugger already exists and is the foundation:
- `jag debug -d host <file>` compiles to a snapshot, launches the **inner**
  `toit.run --debug <snapshot>` as a child process, and relays the VM's `dbg:`
  line protocol over stdin/stdout pipes. No Jaguar agent, no `:9000`, no firmware.
- The relay core `cmd/jag/dbg/` is transport-agnostic behind a `Channel`
  interface: `protocol.go` (parse), `names.go` (offline name resolution),
  `session.go` (relay engine: `Start`/`Methods`/`Do`, alias→verb translation,
  output splitting, pretty-printing). The host `stdioChannel` lives in
  `commands/debug.go` alongside the `runDebug` driver, which dispatches to
  `runREPL` (default) or `runScript` (`--script`).
- The VM emits **numeric** method ids and **raw stack slots**; names are resolved
  offline from `toit tool snapshot bytecodes`. Source positions and named locals
  are not surfaced yet.

**Key clarification (locked):** the web page is served by **`jag` itself** (the
Go CLI), not by the Jaguar agent. The agent's `:9000` server runs on *devices*
and belongs to the deferred device-debug path. For `-d host` there is no agent in
the loop — `jag` is the long-lived process that owns the VM child and the relay,
so it owns the web server too. The host has ample CPU; running jag + its server +
the browser + the VM together is a non-issue.

## Scope

**In scope (v1):**
- A new `--web` flag on `jag debug` (host only). Mutually exclusive with
  `--script`; both default off → interactive CLI REPL (unchanged).
- An embedded `net/http` server in `jag` on an ephemeral `localhost` port; opens
  the user's browser.
- A single self-contained web page (no build step) showing: source with line
  numbers + current-line highlight, clickable-gutter breakpoints, control buttons
  (step/over/out/continue), and a raw variables panel.
- Browser→jag commands and jag→browser state push.
- **Current-line resolution**: a new offline source-position map
  (`absolute_bci → file:line`), produced by a small new toit-side dump and parsed
  by jag, used to translate the paused `(method id, offset)` into a source line.

**Deferred (NOT built now), but architected for:**
- Named locals / decoded values in the variables panel (the "moderate" VM-side
  work — the VM debugger must emit named locals; today only raw `rN=` slots).
- Device debugging served by the Jaguar agent (see "Device symmetry").
- Multi-client, auth, remote access (the server binds `localhost`, single client).

**Out of scope (YAGNI):**
- A frontend framework / build pipeline (npm, bundlers). Vanilla HTML/CSS/JS.
- Syntax highlighting beyond current-line + breakpoint markers (nice-to-have, later).
- WebSocket (SSE + POST is sufficient for one local client; see "Transport").
- DAP / VS Code integration (the structured sink could grow into this later).

## Command surface

```
jag debug [-d host] <file.toit>            # interactive CLI REPL (default, unchanged)
jag debug [-d host] --script <cmds> <file> # scripted (unchanged)
jag debug [-d host] --web <file>           # browser UI (new)
```

- `--web` and `--script` are mutually exclusive; setting both is an error.
- `--web` honors the same `-d host`-only guard and the same compile→launch path.
- Modes are **flag-selected at launch**, never live-synced. The web UI is the
  primary surface for humans; the CLI/script remains for LLM agents and CI.

## Architecture (Approach A)

```
 Browser (vanilla SPA)                 jag (Go)                         VM child
 ┌─────────────────────┐  POST /cmd   ┌───────────────────────┐  pipes ┌─────────┐
 │ source + cur line    │ ───────────▶ │ runWeb (driver)        │ ─────▶ │toit.run │
 │ gutter breakpoints   │              │  • http.Server (embed) │        │ --debug │
 │ step/over/out/cont   │  SSE /events │  • dbg.Session (relay) │ ◀───── └─────────┘
 │ variables panel      │ ◀─────────── │  • PositionMap (lines) │  dbg:  protocol
 └─────────────────────┘              └───────────────────────┘
```

### `cmd/jag/dbg/` (core additions — still no os/exec, pipes, or cobra)

1. **`positions.go` (new)** — pure parse + lookup of source positions:
   - `type PositionMap` mapping `absolute_bci → Position{File string; Line int}`.
   - `func ParsePositions(dump string) PositionMap` — parses the new toit-side
     dump (analogous to `ParseBytecodes`).
   - `func (PositionMap) Locate(entryBci, off int) (Position, bool)` — the current
     line, where `absolute_bci = entryBci + off`. The caller supplies `entryBci`
     from the registry (`Method.EntryBci`) for the paused method id.

2. **`session.go` (extend)** — add a structured **observer** seam so the same
   relay feeds both the text renderer and the web, without changing the protocol
   or the existing text output:
   - `func (s *Session) SetObserver(fn func(Event))` — called for every parsed
     `Event` in addition to existing pretty-printing. `runREPL`/`runScript` leave
     it unset (text behavior unchanged).
   - Small state accessors the web driver reads after each command:
     `func (s *Session) LastPause() (id, off int, ok bool)` and
     `func (s *Session) LastStack() (Event, bool)` (the most recent `dbg:stack`).
   - `func (s *Session) Exited() bool` (already tracked internally as `exited`).

   The web driver uses the observer to accumulate program-output lines and to
   notice pauses/stacks; it uses the accessors to snapshot state after driving a
   command.

### `cmd/jag/commands/` (wiring)

3. **`debug.go` (extend)** — `DebugCmd` gains `--web`; reject `--web && --script`;
   `runDebug` dispatches to a new `runWeb` when `--web` is set.

4. **`debug_web.go` (new)** — the web driver:
   - Builds the `PositionMap` (via a new `sdk.SnapshotPositions` helper, below).
   - Starts an `http.Server` on `127.0.0.1:0` (ephemeral); prints the URL and
     opens the browser (best-effort; the URL is always printed so headless still
     works).
   - Endpoints:
     - `GET /` → the embedded page.
     - `GET /source?file=<path>` → the text of a source file referenced by a
       position (read from disk; `<sdk>/...` resolved against the SDK lib dir).
     - `GET /events` → **SSE** stream pushing `StateUpdate` JSON on every change.
     - `POST /cmd` → a command `{verb, arg}` (step/over/out/continue) or
       `{verb:"break"|"clear", file, line}`; the driver translates a (file,line)
       breakpoint to a method id + offset via the maps and calls `session.Do(...)`,
       then pushes a fresh `StateUpdate`.
   - Serializes commands (one debug action at a time); the VM is single-threaded
     and the relay is synchronous.
   - On program exit (`session.Exited()`), pushes a terminal `StateUpdate` and
     keeps the page up (read-only) until the user closes it / `jag` is stopped.

5. **`commands/web/` (new, embedded via `go:embed`)** — `index.html`, `app.js`,
   `style.css`: render source + current line + gutter breakpoints + controls +
   variables; subscribe to `/events`; POST commands.

6. **`util.go` (extend)** — `func (s *SDK) SnapshotPositions(ctx, snapshot)
   ([]byte, error)` shelling out to the new `toit tool snapshot positions`.

### toit repo (separate change, prerequisite)

7. **`tools/snapshot.toit` — new `positions` subcommand.** Emits, per method, the
   source position of each bytecode, e.g. one line per bytecode:
   `<absolute_bci> <file> <line> <col>`. Reuses the existing program model
   (`program.method-from-absolute-bci`, `method-info.position bci`,
   `method-info.error-path`) already used by `mirror.toit` for stack traces.
   Requires rebuilding the host SDK (the dev already builds it via
   `JAG_TOIT_REPO_PATH`). This is the only cross-repo dependency.

   *Alternative considered:* extend the VM's `dbg:paused` to emit `file:line:col`
   directly (cleaner protocol, no offline map) — deferred as C++/VM work; the
   offline dump is lower risk for v1 and matches the existing offline-name pattern.

## Data model (jag → browser)

`StateUpdate` (pushed over SSE as JSON):
```
{
  "status":   "paused" | "running" | "exited",
  "location": { "file": "examples/simple.toit", "line": 8, "method": "count-to" } | null,
  "breakpoints": [ { "file": "...", "line": 8 }, ... ],
  "variables":  [ { "slot": 0, "value": "3266" }, { "slot": 1, "value": "<obj>" } ],
  "method_id":  448
}
```
The browser also fetches `/source?file=...` when `location.file` changes and
renders it with the highlighted line and breakpoint markers.

## Data flow (one step)

1. User clicks **Step** → `POST /cmd {verb:"step"}`.
2. `runWeb` calls `session.Do("s")`; the relay sends `dbg:step`, drains to the
   next `dbg:paused id off` (the observer records program output along the way).
3. `runWeb` reads `session.LastPause()` → `(id, off)`; looks up
   `Method.EntryBci` for `id`; `PositionMap.Locate(entryBci, off)` → `(file,line)`.
4. `runWeb` issues `session.Do("i")` to refresh the frame → `session.LastStack()`
   → variables.
5. `runWeb` builds a `StateUpdate` and pushes it over SSE; the page highlights the
   new line and updates the variables panel.

## Breakpoints from the gutter

Clicking line *L* in file *F* → `POST /cmd {verb:"break", file:F, line:L}`. The
driver maps `(F,L)` to the enclosing method + offset using the position map
(reverse of `Locate`: the **first / lowest-offset** bytecode whose position is on
line `L` of file `F`), resolves the method id via the registry, and calls
`session.Do("b <id> <off>")`. Clearing is
the same with `clear`. The driver tracks the active breakpoint set and echoes it
in every `StateUpdate`. If a line has no executable bytecode, the click is
rejected with a notice (no breakpoint set).

## Error handling

| Situation | Behavior |
|-----------|----------|
| `--web` and `--script` both set | error before launch |
| `-d` ≠ host | existing `device debugging is not yet supported (only -d host)` |
| Browser can't be opened | URL printed to console; server still serves |
| Source file unreadable / `<sdk>` path unresolved | page shows "source unavailable for `<path>:line`"; stepping still works |
| Breakpoint on a non-executable line | command rejected, notice pushed; no break set |
| SDK lacks the `positions` dump (old SDK) | `--web` fails at startup with a clear "rebuild the debug SDK" message; CLI mode unaffected |
| Program exits | terminal `StateUpdate {status:"exited"}`; page goes read-only |
| VM launch / compile failure | same as today (diagnostics, non-zero exit, no server started) |

## Device symmetry (future — not built)

The web front-end (HTML/JS) and the `StateUpdate`/command shapes are designed to
be **transport- and host-agnostic**, mirroring the `Channel` seam:

| | Host (this spec) | Device (future) |
|---|---|---|
| Serves the page | `jag` (embedded `net/http`) | Jaguar agent (`:9000`) |
| Reaches the debugger | stdio pipes → `toit.run` | in-image debug supervisor + VM primitives |
| Source positions | offline `snapshot positions` dump | same map, shipped/queried on device |
| Web page + protocol | **same** | **same** |

So building the page now, served by jag over the host path, is reusable when the
device path lands — only the server host and the debugger transport change.

## Testing strategy

- **Unit (no VM, no browser):**
  - `positions.go` — `ParsePositions` against a captured dump fixture; `Locate`
    returns the known `(file,line)` for `count-to` at a given offset.
  - `session.go` — the observer fires for each event; `LastPause`/`LastStack`
    reflect the most recent pause/stack, driven against the fake `Channel`.
  - `debug_web.go` — the `(file,line) → (id,off)` breakpoint mapping and the
    `StateUpdate` JSON shape, with a fake session/maps. The HTTP handlers tested
    with `httptest` (POST a command, assert the pushed `StateUpdate`), no real VM.

- **Integration (real VM, gated like the existing host-debugger test):**
  - Drive `runWeb`'s command path (not the browser) end to end against
    `testdata/count_to.toit`: set a breakpoint by `(file,line)`, continue, step,
    assert the `StateUpdate` location advances line-by-line and the program output
    is captured. Skipped when no debug-capable SDK (and no `positions` dump) is
    found.

- **Manual:** `jag debug --web testdata/count_to.toit`, click a gutter breakpoint,
  step, confirm the highlighted line tracks and variables update.

## File structure

toitlang/jaguar:
- `cmd/jag/dbg/positions.go` — **new**: `PositionMap`, `ParsePositions`, `Locate`.
- `cmd/jag/dbg/positions_test.go` — **new**.
- `cmd/jag/dbg/session.go` — **modified**: observer seam + `LastPause`/`LastStack`/`Exited`.
- `cmd/jag/dbg/session_test.go` — **modified**: observer/accessor tests.
- `cmd/jag/commands/debug.go` — **modified**: `--web` flag + dispatch.
- `cmd/jag/commands/debug_web.go` — **new**: `runWeb`, HTTP handlers, browser launch.
- `cmd/jag/commands/web/{index.html,app.js,style.css}` — **new**: embedded page.
- `cmd/jag/commands/util.go` — **modified**: `SnapshotPositions` helper.
- `cmd/jag/commands/debug_web_integration_test.go` — **new**: gated end-to-end.
- `docs/jag-debug.md` — **modified**: document `--web`.

toitlang/toit (prerequisite, separate change + SDK rebuild):
- `tools/snapshot.toit` — **new** `positions` subcommand.

## Assumptions & prerequisites

- A debug-capable SDK (the `feature/host-debugger` branch) **plus** the new
  `snapshot positions` subcommand, discovered via `JAG_TOIT_REPO_PATH`.
- One local browser client; server binds `127.0.0.1` on an ephemeral port.
- The existing host-debugger relay, `Channel` seam, REPL, and `--script` mode are
  unchanged; `--web` is an additional, parallel driver.
