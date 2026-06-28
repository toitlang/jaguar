# `jag debug` — host bytecode debugger

`jag debug` runs a Toit program on your host machine under the Toit VM's
bytecode debugger and gives you a small `gdb`-style command loop: set
breakpoints by method name, step, and inspect stack frames.

```
jag debug [-d host] <file.toit> [--script <cmds.txt>]
```

Only host debugging is supported today. `-d` defaults to `host`; any other
value reports `device debugging is not yet supported (only -d host)`.

## Prerequisites

You need a Toit SDK built **with the debugger** (the `feature/host-debugger`
branch of the `toit` repo, or any SDK once it merges). For development, point
Jaguar at your toit checkout so it uses that SDK and skips the version check:

```bash
export JAG_TOIT_REPO_PATH=$HOME/workspaceToit/toit   # uses $JAG_TOIT_REPO_PATH/build/host/sdk
```

If the SDK lacks the debugger, the session fails at launch with a message
pointing at the SDK requirement.

## How it works (one paragraph)

`jag debug` compiles your file to a snapshot, then launches the VM on that
snapshot in debug mode. Debugging a snapshot (rather than the raw `.toit`) is
what gives stable, program-relative breakpoint locations. Method **names** are
resolved offline from the snapshot — the VM only knows numeric ids — so you can
break on `count-to` instead of a number.

## Interactive use

```bash
jag debug -d host myprogram.toit
```

You get a `dbg>` prompt. The program starts **paused at entry**, so you can set
breakpoints before anything runs.

```
dbg> m                 # list your methods (see note below)
dbg> b count-to        # breakpoint on a method by name
dbg> c                 # continue → stops at count-to
dbg> i                 # inspect the current frame (registers)
dbg> s                 # step into
dbg> n                 # step over
dbg> f                 # finish: run until the current frame returns
dbg> c                 # continue to the next breakpoint / program end
dbg> q                 # quit (detaches and exits)
```

## Command reference

| Alias | Full name | Meaning |
|-------|-----------|---------|
| `b <name\|id> [off]` | `break` | set a breakpoint (offset defaults to 0) |
| `d <name\|id> [off]` | `clear` | clear a breakpoint |
| `c` | `continue` | resume |
| `s` | `step` | step into |
| `n` | `over` | step over (skip callees) |
| `f` / `fin` | `out` | run until the current frame returns |
| `i [frame]` | `inspect` | inspect a stack frame (default 0) |
| `m [all]` | `methods` | list methods (see below) |
| `help` | | show this command list |
| `q` | `quit` | detach and exit |

You can break by method **name** or by numeric **id**. Names are what you'll
normally use; ids are there for methods with no source name.

### About `m` (methods)

The compiled image contains your program **plus the entire SDK** — hundreds of
methods. A bare `m` therefore lists only **your** methods:

```
dbg> m
Your methods (2 of 575; 'm all' for every method incl. SDK):
   447  entry_bci=263  arity=0  main
   448  entry_bci=285  arity=1  count-to
```

Use `m all` if you really need the full registry (e.g. to break on an SDK
method by id). In normal use you rarely need `m` at all — you already know your
function names from the source, so just `b <name>`.

## Scripted / CI use

`--script` feeds newline-delimited commands to the same engine and prints the
transcript, then exits. Blank lines and lines starting with `#` are ignored.

```bash
cat > cmds.txt <<'EOF'
b count-to
c
i
c
EOF
jag debug -d host --script cmds.txt myprogram.toit
```

This is how the end-to-end test drives a real session
(`cmd/jag/commands/debug_integration_test.go`).

## Output

The VM's protocol responses and your program's own output share one stream.
`jag debug` pretty-prints the protocol lines (`paused in count-to at off 0
(break)`, `stack off=…`, `ok: …`, `error: …`) and forwards everything else —
your program's `print`s — verbatim. A program line that happens to begin with
`dbg:` would be misread as a protocol line; this is a known limitation.

## Notes & limitations

- **Program exit:** when the program runs to completion, the debugger prints
  `program exited` and the session ends; further commands are ignored rather
  than failing against the gone VM.
- **Quitting:** `q` (or end-of-input) detaches and terminates the VM, even when
  paused at a breakpoint — the debugged program does not keep running.
- **Breakpoints vs. entry:** the program starts *paused at entry*
  (`#-1` / `__entry__main`). Set your breakpoints, then `c` (continue) to reach
  them — stepping (`s`) from the entry point walks the runtime's entry stub, not
  your `main`.
- **Settling:** after a `continue` that runs the program to completion there is
  a brief (~0.5s) pause before the prompt returns; the debug VM does not exit on
  its own, so the driver waits for output to go quiet.
- **Not yet supported:** device debugging (`-d <device>`), conditional
  breakpoints, watchpoints, and expression evaluation beyond `inspect`.
  Inspected non-integer values print as `<obj>` (no class-name resolution).

## Web UI (`--web`)

`jag debug --web <file.toit>` opens a browser-based debugger instead of the
REPL. jag serves a self-contained page on an ephemeral `localhost` port (the URL
is printed to the console; the browser opens automatically when possible).

The page shows the program source with the current line highlighted as you step,
lets you set/clear breakpoints by clicking the line-number gutter, drives
execution with the Continue / Step / Over / Out buttons, and shows the current
frame's raw register slots (`r0`, `r1`, …) in the variables panel. The debugged
program's own output stays on the launch console.

`--web` is mutually exclusive with `--script`, and like the rest of `jag debug`
it supports `-d host` only. It requires a debug-capable SDK that includes the
`snapshot positions` tool; on an older SDK, `--web` fails at startup with a
"rebuild the debug SDK" message (the REPL/script modes are unaffected).

Named local variables are not yet decoded — the panel shows raw VM stack slots.

## Design

The architecture (a transport-agnostic core behind a `Channel` interface, so a
future device transport is a new implementation rather than a rewrite) is
described in `docs/superpowers/specs/2026-06-27-jag-host-debugger-design.md`.
