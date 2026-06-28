# `jag debug` — Specification

**Status:** implemented on branch `feature/jag-host-debugger` (CLI + `--web`).
**Audience:** PR reviewers. The "how" lives in [debug_design.md](debug_design.md);
the user guide is [jag_debug.md](jag_debug.md).

This document is the distilled record of what `jag debug` is, the decisions taken
and why, the tradeoffs accepted, what is deliberately out of scope, and the
least‑invasive ways the design can grow.

## Goal

Give operators an interactive bytecode debugger for Toit programs run on the host
VM — `jag debug -d host <file>` — built on the Toit VM's existing `--debug` mode
and `dbg:` line protocol. Two front‑ends share one relay core:

- a `gdb`‑style REPL (plus a `--script` mode for CI), and
- a browser UI (`--web`): source with the current line highlighted as you step,
  click‑the‑gutter breakpoints, step/over/out/continue, and a variables panel
  showing real frame values.

The host path is the fast loop: `jag run -d host` already runs the VM locally, so
debugging there needs no firmware and no new VM primitives.

## Scope

**In scope (implemented):**
- A `jag debug` subcommand, `-d host` only.
- Three launch‑selected modes: REPL (default), `--script <file>`, `--web`.
- gdb‑style aliases; offline method‑name resolution from the snapshot.
- A transport‑agnostic relay core behind a `Channel` interface (host stdio pipes
  the only transport today).
- `--web`: an embedded `net/http` server + a single `go:embed` page; SSE for
  state push, POST for commands; current‑line highlighting via an offline
  source‑position map; gutter breakpoints; line‑granularity stepping.
- **Variable values** in the panel: typed scalars (`null`/`true`/`false`,
  integers, doubles), strings, and heap objects shown as `<obj:ClassName>` —
  resolved offline. (This supersedes the original v1, which showed raw `rN=`
  slots and `<obj>`.)

**Deferred, but architected for (not built):**
- Device debugging (`jag debug -d <esp32>`): a future `Channel` implementation
  plus an in‑image debug supervisor and new VM primitives. The web page and the
  `StateUpdate`/command shapes are already transport‑ and host‑agnostic.
- Named locals in the variables panel (needs compiler‑emitted local metadata;
  today the panel keys on stack slot index).

**Out of scope (YAGNI):**
- Any firmware / device‑side Toit / new VM C++ primitives for the host path.
- Debug Adapter Protocol (DAP) / editor integration.
- Conditional breakpoints, watchpoints, expression evaluation beyond `inspect`.
- A frontend framework or build pipeline — vanilla HTML/CSS/JS, no npm.
- WebSocket / multi‑client / auth (single local client; binds `127.0.0.1`).
- Debugging a raw `.toit` directly (we always compile to a snapshot first).

## Final decisions taken — and why

| Decision | Rationale |
|---|---|
| **`jag debug` subcommand**, not a `run --debug` flag. | Mirrors how every machine‑targeting jag verb takes `-d`, so the same command extends to `jag debug -d <device>` later. Leaves `run`'s logic untouched. |
| **`-d host` only**; any other value → `device debugging is not yet supported (only -d host)`. | A flag‑based guard makes the device path a future `Channel` impl, not a rewrite. |
| **Three modes, flag‑selected at launch**, never live‑synced. | The web UI is the human surface; the REPL/script stay for LLM agents and CI. Mode switching adds state for no real gain. |
| **Compile to a snapshot first**, always — never debug raw `.toit`. | Stable program‑relative bytecode indices and clean method ids — the invariant the VM debugger was built around. Snapshot also lands in jag's existing cache. |
| **Launch the inner `<sdk>/lib/toit/bin/toit.run --debug <snap>`**, not `toit run --debug`. | The multiplexer's `run` has no `--debug`; only the inner runner accepts it (sets `OEVM_DEBUG`). Needs a `ToitRunDebug` helper because `ToitRun` hard‑codes `--`, which would hide `--debug` from the VM. |
| **Transport‑agnostic core `cmd/jag/dbg/`** behind a `Channel` interface (`Send`/`Lines`/`Close`); no `os/exec`, pipes, cobra, or `net/http`. | The relay unit‑tests against a fake in‑memory `Channel`; the device path is a new file, not a rewrite. |
| **VM emits numeric ids only; names/positions/class‑names resolved offline** from the snapshot (`tool snapshot bytecodes` / `positions` / `class-names`). | Matches `jag decode` and the proven Python PoC; keeps the wire protocol minimal and the VM change small. The protocol stays "VM numeric, names resolved offline." |
| **Web transport = SSE (jag→browser state) + POST (browser→jag commands)** on `127.0.0.1:0`. | Sufficient for one local client; no WebSocket, no auth. |
| **Single `go:embed` page**, `Cache-Control: no-store`. | No build step, no deps; `no-store` avoids stale JS after a rebuild. |
| **`absolute_bci = method.EntryBci + off`** is the join between the live pause and the offline maps. | Lets jag resolve a paused `(id, off)` to a source line, and a gutter click back to a `(method id, off)` breakpoint, all offline. |
| **Line‑granularity stepping** (loop the VM's per‑bytecode step until a genuinely new source line). | Most bytecodes carry the method's *declaration*‑line as a fallback, so a single VM step bounces the marker to `main:`. Looping to a new line gives "one click = one line." |
| **Over/Out skip `<sdk>/` frames** (`allowSDK=false`); In may descend. | Stepping off the end of `main` runs to completion instead of dragging the user through runtime teardown. |
| **`--web` console shows only the program's own output** (quiet mode). | One line‑step fans out into many VM‑level step/over commands; echoing every `ok:`/`paused` ack would bury the program's output. The browser already renders protocol state. |
| **Heap objects → `<obj:<class_id>>` from the VM, name resolved offline**; strings emitted quoted/escaped + a quote‑aware wire parser. | Only the VM can read a heap value, but keeping names offline preserves the project's resolution model. Strings need quoting because the register wire format is whitespace‑delimited. |

## Tradeoffs accepted

- **Idle‑settle latency.** After a resume, a 600 ms idle timeout lets a
  run‑to‑completion finish before reporting status. The debug VM does not exit on
  program end (it blocks on stdin), so without the settle a terminal resume would
  hang. Cost: ~0.5 s before the prompt/status returns.
- **Synchronous relay, no background reader.** One command at a time, simple state
  model, no races — but a breakpoint hit while "running" isn't observed until the
  next command. The UI keeps controls live so the next Continue catches up.
- **Gutter breakpoint → lowest bci on the line.** When several bytecodes share a
  line, the breakpoint targets the lowest. Matches gdb; pragmatic.
- **Faithful, not pretty, line stepping.** Multi‑line list literals highlight
  their inner rows before the assignment line (sub‑expressions evaluate first),
  and "run to main" starts you *past* the first statement's already‑built
  sub‑expressions. Each marker is correct for the executing bytecode; the order
  can surprise. Left as‑is by decision (cosmetic; a real fix needs offline
  statement‑range info or different run‑to‑main heuristics).
- **No path normalization.** jag echoes back the exact path the compiler recorded
  (relative or absolute, or `<sdk>/…`), so `/source` lookups stay end‑to‑end
  consistent without a normalization layer.

## Known limitations

- **Exceptions during debug** work (no crash/hang): an unhandled throw prints the
  VM's `EXCEPTION` + backtrace to the console (it is program output, so it shows
  even in `--web` quiet mode), and the UI goes `done → exited`. Two gaps, left as
  known limitations:
  - Stepping **Over** a `catch:` block overshoots to `done` instead of stopping
    at the next line — the exception unwind bypasses the VM's over/step
    depth‑tracking, so the over degrades to a continue. Workaround: a breakpoint
    after the catch, or continue.
  - The browser shows only `done`/`exited` for an exceptional exit; the exception
    text is console‑only.
- A program line that begins with `dbg:` is misread as a protocol line.

## Least‑invasive future work

- **Device debugging** — a new `Channel` implementation (HTTP against the Jaguar
  agent's `:9000`, or UART) plus the in‑image debug supervisor and VM primitives.
  The relay, protocol parser, name/position/class resolution, REPL, alias scheme,
  web page, and `StateUpdate` shapes are unchanged; only the server host and the
  debugger transport differ.
- **Named locals** — emit local‑variable metadata in the snapshot (compiler work),
  then key the variables panel on names instead of slot indices.
- **Send‑level highlighting** — map send (invoke*) bytecodes to source columns for
  sub‑line highlighting (needs compiler position work).
- **Web exception banner** — capture the VM's `EXCEPTION`/backtrace into a
  `StateUpdate` field and show a banner so an exceptional exit is visible in the
  browser, not just the console. Self‑contained.
- **Over‑across‑`catch:`** — re‑arm the over/step stop condition after an exception
  unwind (VM‑side) so stepping over a caught throw lands on the next line.
- **`--script` exit code** — propagate mid‑script relay errors to a non‑zero exit
  for CI.

## Cross‑repo dependency (toit prerequisite)

The host debugger relies on the Toit VM's `--debug` mode (branch
`feature/host-debugger` in the `toit` repo) and on two offline snapshot dumps
emitted by `tools/toitp.toit`:

- `tool snapshot positions` — one line per bytecode, `<absolute_bci> <path>
  <line> <col>` — drives current‑line highlighting and gutter→bci mapping.
- `tool snapshot class-names` — `<class_id> <name>` per line — resolves the
  numeric class ids the VM emits for heap‑object registers.

Plus the VM change that makes `emit_stack` report typed values and `emit_string`
emit quoted strings. After editing the VM (`src/debugger.cc`) rebuild with
`ninja sdk/bin/toit`; after editing `tools/toitp.toit` rebuild with
`ninja generated/toit.snapshot sdk/bin/toit` (ninja, not make — make does not
track transitive `.toit` imports). For development, jag discovers the
debug‑capable SDK via `JAG_TOIT_REPO_PATH` → `$JAG_TOIT_REPO_PATH/build/host/sdk`,
which bypasses the version check. An SDK that lacks these subcommands fails `--web`
at startup with a "rebuild the debug SDK" message; the REPL/script modes are
unaffected.
