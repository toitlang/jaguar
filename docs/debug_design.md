# `jag debug` — Design

**Status:** implemented on branch `feature/jag-host-debugger`.
**Audience:** PR reviewers. The "what & why" is in [debug_spec.md](debug_spec.md);
the user guide is [jag_debug.md](jag_debug.md).

This describes how the host debugger works: the architecture, the VM protocol and
the offline maps that join to it, line stepping, variable values, and the `--web`
driver.

## Architecture

A transport‑agnostic relay core (`cmd/jag/dbg/`) with three thin drivers in
`cmd/jag/commands/` (REPL, `--script`, `--web`). The core never touches pipes,
sockets, or cobra — it speaks only to a `Channel`.

```
 Browser (vanilla SPA)                 jag (Go)                          VM child
 ┌─────────────────────┐  POST /cmd   ┌────────────────────────┐  pipes  ┌─────────┐
 │ source + cur line   │ ───────────▶ │ runWeb (web driver)    │ ──────▶ │toit.run │
 │ gutter breakpoints  │              │  • http.Server (embed) │         │ --debug │
 │ step/over/out/cont  │  SSE /events │  • dbg.Session (relay) │ ◀────── └─────────┘
 │ variables panel     │ ◀─────────── │  • PositionMap/Classes │   dbg:  line protocol
 └─────────────────────┘              └────────────────────────┘
        REPL / --script drivers use the same dbg.Session, no HTTP.
```

### Core — `cmd/jag/dbg/` (no os/exec, pipes, cobra, net/http)

- **`channel.go`** — the device seam:
  ```go
  type Channel interface {
      Send(cmd string) error   // one dbg: request line to the target
      Lines() <-chan string    // every line the target emits (dbg: responses + app stdout)
      Close() error
  }
  ```
- **`protocol.go`** — pure parse of `dbg:` lines → `Event{Kind, Mode, ID, Off,
  Regs, Verb, Msg, Text}`. Kinds: `ready`, `paused`, `stack`, `ok`, `error`,
  `app` (the program's own output), `other`. Includes `parseRegs` (see
  *Variable values*).
- **`names.go`** — offline name resolution: parses `tool snapshot bytecodes`
  (name ↔ entry_bci, plus an `EntrySDK` flag) and combines it with the runtime
  registry (id ↔ entry_bci) into a `Resolver` (name ↔ id).
- **`positions.go`** — `PositionMap`: `ParsePositions` reads the positions dump;
  `Locate(entryBci, off)` → `(file, line)`; `LineToAbs(file, line)` → lowest
  absolute_bci (gutter → breakpoint); `FirstLineInRange` (run‑to‑main);
  `MethodForAbs` (abs → enclosing method id + offset).
- **`classnames.go`** — `ClassNames`: `ParseClassNames` reads the class‑names
  dump; `Resolve` rewrites `<obj:N>` → `<obj:ClassName>`.
- **`session.go`** — the relay engine. Owns a `Channel` + the name map. `Start`
  waits for `dbg:ready`; `Methods` fetches the registry; `Do(input)` translates
  an alias→verb and a name→id, sends it, and drains the response. Splits output:
  `dbg:` lines are parsed/pretty‑printed, everything else is the program's own
  output. A structured **observer** seam (`SetObserver`) feeds the web driver
  every parsed event; state accessors (`LastPause`/`LastStack`/`Exited`/
  `PauseGen`/`MethodName`/`Registry`) snapshot state after a command. `SetQuiet`
  suppresses protocol text on the console (web mode — see *Console*).

### Drivers — `cmd/jag/commands/`

- **`debug.go`** — `DebugCmd()`: parse `-d`/`--script`/`--web`; reject non‑host and
  `--web && --script`; compile → snapshot; build the name map; launch the VM via
  `sdk.ToitRunDebug`; construct the host `stdioChannel`; run REPL, `runScript`, or
  `runWeb`. Sets `session.SetQuiet(true)` before `Start` when `--web`.
- **`stdioChannel`** — the host `Channel`: wraps the child VM's stdin/stdout pipes.
- **`debug_web.go`** — `runWeb` + `webDriver` + `webServer`: builds the
  `PositionMap` + `ClassNames`, starts `http.Server` on `127.0.0.1:0`, opens the
  browser (best‑effort; URL always printed), and serves the endpoints below.
- **`package_source.go`** — maps `<pkg:..>/rel` source paths to on‑disk files via
  `package.lock`, so stepping into a package method shows its source.
- **`web/{index.html,app.js,style.css}`** — the embedded page (`go:embed`).
- **`util.go`** — `SnapshotBytecodes` / `SnapshotPositions` / `SnapshotClassNames`
  helpers (shell out to the toit tool).

## VM protocol & the offline join

The Toit VM launched with `--debug` enters a line‑based mode, emitting `dbg:`
lines on stdout interleaved with the program's output (split on the `dbg:`
prefix). Requests: `dbg:methods`, `dbg:break <id> <off>`, `dbg:clear <id> <off>`,
`dbg:continue`, `dbg:step|over|out`, `dbg:inspect [frame]`. Responses:
`dbg:ready`, `dbg:ok <verb>`, `dbg:paused break|step <id> <off>`, `dbg:stack
off=<n> r0=<v> …`, `dbg:error <msg>`. After `dbg:ready` the VM pauses at the
entry stub (`dbg:paused break -1 0`; method id `-1` has no registry entry / source
line). `dbg:methods` emits non‑prefixed registry lines `<id> <entry_bci> <arity>`
then `dbg:ok methods`.

**The join.** The VM speaks numeric ids and offsets; everything human is resolved
offline against the snapshot. The bridge is:

```
absolute_bci = Registry[id].EntryBci + off
```

- **Paused → source line:** `Locate(EntryBci, off)` looks up `absolute_bci` in the
  positions dump → `(file, line)`.
- **Gutter click → breakpoint:** `LineToAbs(file, line)` → lowest `absolute_bci`;
  `MethodForAbs(registry, abs)` → `(id, off = abs − EntryBci)`; send
  `dbg:break <id> <off>`.

### Offline dumps (toit `tools/toitp.toit`)

- `tool snapshot bytecodes <snap>` — per‑method blocks with names + entry_bci
  (method‑name resolution; pre‑existing).
- `tool snapshot positions <snap>` — one line per bytecode `<absolute_bci> <path>
  <line> <col>`. `<path>` is the compiler's error‑path: the exact path passed to
  jag for user files (relative stays relative), `<sdk>/…` for the SDK, `<pkg:..>/…`
  for package files.
- `tool snapshot class-names <snap>` — `<class_id> <name>` per line, from
  `program.class-tags` / `program.class-name-for`.

## Line‑granularity stepping

The VM steps **one bytecode at a time**, and most bytecodes carry the method's
*declaration*‑line as a position fallback — so a single VM step makes the marker
bounce to `main:`. `stepToNewLine` loops the VM primitive until `atNewLine`
reports a genuinely meaningful stop:

- a **different method** (stepped into or out of a call), or
- within the same method, a source line that is **neither the start line nor the
  declaration line** (the fallback used for filler bytecodes).

It stops early on program end (idle‑settle → `done`), a breakpoint hit
(`LastPauseReason == "break"`), or a safety cap (`maxSteps`). **In** passes
`allowSDK=true` (may descend into the SDK); **Over/Out** pass `allowSDK=false`, so
`atNewLine` skips `<sdk>/…` frames — stepping off the end of `main` runs through
the runtime teardown to completion instead of surfacing it.

**Run‑to‑main.** On `--web` load the page opens paused at `main`'s first statement,
not the entry stub: `FirstLineInRange` finds the declaration line, then the first
line past it within `main`'s bci range, and a one‑shot breakpoint drives there.

## Variable values

`dbg:stack` reports each frame register as `rN=<value>`. The VM (`emit_stack`)
formats by type so jag receives real values instead of a generic `<obj>`:

- `null` / `true` / `false`; integers (smi); doubles (`%g`).
- Strings via `emit_string`: a double‑quoted, escaped token (`\" \\ \n \r \t`,
  `\xNN` for control chars), capped at 128 chars — so the value stays
  whitespace‑delimited on the wire.
- Any other heap object: `<obj:<class_id>>` (the **numeric** class id).

jag resolves these:

- **`parseRegs`** (protocol.go) replaces the old `r(\d+)=(\S+)` regex — which would
  split a string value on its interior spaces — with a scanner that reads bare
  tokens up to whitespace and double‑quoted tokens to the matching unescaped
  quote, resolving the escapes.
- **`ClassNames.Resolve`** rewrites `<obj:16>` → `<obj:String_>`, `<obj:47>` →
  `<obj:Point>`, etc., using the class‑names dump. Scalars and unknown ids pass
  through unchanged.

The VM's class ids match the offline snapshot's class ids, so the numeric id the
VM emits resolves directly through the dump.

## `--web` driver

**Endpoints:** `GET /` (the embedded page), `GET /app.js` / `/style.css`,
`GET /source?file=<path>` (file text; `<sdk>/…` resolved against the SDK lib dir,
`<pkg:..>/…` via `package.lock`), `GET /events` (SSE state stream), `POST /cmd`
(`{verb, file, line}`).

**State.** `snapshotState` builds a `StateUpdate` after each command:
```json
{ "status": "paused|done|exited",
  "location": { "file": "...", "line": 8, "method": "main" } | null,
  "breakpoints": [ { "file": "...", "line": 8 } ],
  "variables":  [ { "slot": 0, "value": "<obj:Point>" }, { "slot": 1, "value": "42" } ],
  "method_id": 525, "entry_file": "simple.toit" }
```
`entry_file` (the entrypoint path) lets the page show source on first paint while
paused at the entry stub (which has no resolvable `location`). Commands are
serialized under a mutex (the VM is single‑threaded, the relay synchronous).
`status` is `paused`, `done` (a resume settled on idle — ran to completion), or
`exited`.

**One step:** `POST /cmd {verb:"over"}` → `stepToNewLine` → `LastPause` →
`Locate` → `LastStack` (after a refresh `inspect`) → `ClassNames.Resolve` on each
register → `StateUpdate` pushed over SSE → page re‑highlights and updates the
panel.

**Console (quiet mode).** In `--web` the browser is the UI, so the console shows
only the program's own output. Without this, one line‑step — which loops many
VM‑level step/over commands through SDK teardown — would flood the console with
`ok: over` / `paused in …` acks and bury the program's `print`s. `session.print`
forwards only `KindApp` events to the console when quiet; the observer still feeds
the browser every event.

## Idle‑settle & exit

The debug VM does **not** exit on program completion — it blocks reading stdin. So
resume verbs use a 600 ms idle timeout (`drainOrSettle`): if no new pause arrives,
the program ran to completion (status `done`). The driver closes stdin to let the
VM exit (status `exited`). `inspect` is gated so jag never inspects a
still‑running VM after an idle‑settle resume.

## Testing

- **Unit (no VM):** `ParseLine`/`parseRegs` (incl. quoted strings, escapes,
  embedded `rN=`), `ParsePositions`/`Locate`/`FirstLineInRange`,
  `ParseClassNames`/`Resolve`, the observer + `LastPause`/`LastStack`, quiet mode,
  and the `(file,line) → (id,off)` breakpoint mapping + `StateUpdate` shape
  (httptest, fake `Channel` — no process).
- **Integration (real VM, gated on a debug‑capable SDK):** drive the relay against
  `testdata/count_to.toit` — break by `(file,line)`, continue, step, assert the
  location advances and program output is captured. Skipped with a clear message
  when no debug SDK is found.

## File map

`cmd/jag/dbg/`: `channel.go`, `protocol.go`, `names.go`, `positions.go`,
`classnames.go`, `session.go` (+ `_test.go`).
`cmd/jag/commands/`: `debug.go`, `debug_web.go`, `package_source.go`, `util.go`,
`web/{index.html,app.js,style.css}` (+ `_test.go`, `testdata/count_to.toit`).
`toit` repo: `src/debugger.cc` / `debugger.h` (typed `emit_stack`, `emit_string`),
`tools/toitp.toit` (`positions`, `class-names` subcommands).
