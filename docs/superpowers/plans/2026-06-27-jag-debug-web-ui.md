# jag Debug Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `jag debug --web <file.toit>`: a browser view of a host debug session with source + current-line highlight, clickable-gutter breakpoints, step/over/out/continue controls, and a raw variables panel.

**Architecture:** `jag` (the Go CLI) embeds a `net/http` server on an ephemeral `localhost` port. The existing transport-agnostic relay core (`cmd/jag/dbg/`) drives the VM child; a new web driver (`runWeb`) translates browser commands to relay calls and pushes state to the page over SSE. A new offline source-position map (`absolute_bci → file:line`), produced by a new toit-side `snapshot positions` dump, resolves the paused `(method id, offset)` to a source line. The web front-end is transport/host-agnostic so the Jaguar agent can reuse it for future device debugging.

**Tech Stack:** Go 1.16 (stdlib `net/http`, `embed`, `httptest`), cobra; vanilla HTML/CSS/JS (no build step); Toit (`tools/toitp.toit`, the `snapshot` tool CLI) in the toitlang/toit repo.

## Global Constraints

- **Repo:** all jag changes on branch `feature/jag-host-debugger` (do NOT raise a PR until the web UI is done — CLI + web are one deliverable). The toit-side change is on the toit repo's `feature/host-debugger` branch.
- **SDK discovery:** jag finds the debug-capable SDK via `JAG_TOIT_REPO_PATH=~/workspaceToit/toit` → `build/host/sdk`. Set this env var in every shell that runs `jag debug` or the gated integration tests.
- **`dbg` package purity:** `cmd/jag/dbg/` must NOT import `os/exec`, pipes, `net/http`, or cobra. It stays a pure protocol/parse/relay core behind the `Channel` interface. The web server lives in `cmd/jag/commands/`.
- **Modes are flag-selected at launch, never live-synced.** No flag → CLI REPL (unchanged); `--script` → scripted (unchanged); `--web` → browser. `--web` and `--script` are mutually exclusive.
- **`-d host` only:** `--web` honors the existing guard — any non-host `-d` → error `device debugging is not yet supported (only -d host)`.
- **Server binds `127.0.0.1` on an ephemeral port (`:0`), single local client.** No auth, no WebSocket (SSE + POST only).
- **License header** on every new Go and Toit file (copy the 3-line `Copyright (C) 2026 Toitware ApS.` header from any existing file in the same package; the toit repo uses its own LGPL header — copy it from `tools/toitp.toit`).

---

## File Structure

toitlang/toit (prerequisite, separate change + SDK rebuild):
- `tools/toitp.toit` — **modified**: add a `positions` subcommand to the `snapshot` command (alongside the existing `bytecodes` subcommand). One line per bytecode: `<absolute_bci> <error_path> <line> <col>`.

toitlang/jaguar:
- `cmd/jag/dbg/positions.go` — **new**: `Position`, `PositionMap`, `ParsePositions`, `Locate`, `LineToAbs`, `MethodForAbs`.
- `cmd/jag/dbg/positions_test.go` — **new**.
- `cmd/jag/dbg/session.go` — **modified**: observer seam + `LastPause`/`LastStack`/`Exited`/`MethodName` accessors.
- `cmd/jag/dbg/session_test.go` — **modified**: observer/accessor tests.
- `cmd/jag/commands/util.go` — **modified**: `SnapshotPositions` helper.
- `cmd/jag/commands/web/{index.html,app.js,style.css}` — **new**: embedded page.
- `cmd/jag/commands/debug_web.go` — **new**: `runWeb`, `StateUpdate`, breakpoint mapping, HTTP handlers, browser launch.
- `cmd/jag/commands/debug_web_test.go` — **new**: unit tests for mapping + handlers (httptest, no VM).
- `cmd/jag/commands/debug.go` — **modified**: `--web` flag + dispatch.
- `cmd/jag/commands/debug_cmd_test.go` — **modified**: `--web` flag + mutual-exclusion tests.
- `cmd/jag/commands/debug_web_integration_test.go` — **new**: gated end-to-end (real VM).
- `docs/jag-debug.md` — **modified**: document `--web`.

---

## Task 1: toit `positions` subcommand + SDK rebuild + capture fixture

**Files:**
- Modify: `~/workspaceToit/toit/tools/toitp.toit` (add `positions-command` near the `bytecodes-command`, ~line 293, and register it with `snapshot-command.add`).

**Interfaces:**
- Produces: a new SDK tool `toit tool snapshot positions <snapshot>` that prints, for each method, one line per bytecode boundary: `<absolute_bci> <error_path> <line> <col>`. Consumed offline by `dbg.ParsePositions` (Task 2) and by `sdk.SnapshotPositions` (Task 4).

**Background (verified in the toit source):**
- `program.methods` is a `List` of `ToitMethod`; each has `.id`, `.bytecodes` (ByteArray), and `.absolute-bci-from-bci bci/int -> int` (returns `id + HEADER-SIZE + bci`).
- `program.method-info-for id/int -> MethodInfo` returns the `MethodInfo`, which has `.error-path/string` and `.position relative-bci/int -> Position` (falls back to the method's default `position` when a bci has no explicit entry). `Position` has `.line` and `.column`.
- Bytecodes are variable-length: step by `BYTE-CODES[opcode].size` (top-level `BYTE-CODES` list, same as `ToitMethod.output` does). The VM's `dbg:paused <id> <off>` reports `off` == the relative bci (index within `bytecodes`), so `absolute_bci = method.absolute-bci-from-bci off`. Iterating only bytecode boundaries is correct because a pause is always at a boundary.

- [ ] **Step 1: Add the `positions` subcommand to `build-command`**

In `tools/toitp.toit`, immediately after the block that ends with `snapshot-command.add bytecodes-command` (~line 309), insert:

```toit
  positions-command := cli.Command "positions"
      --help="""
          Print the source position of every bytecode, one line per bytecode:
          <absolute_bci> <error_path> <line> <col>.

          Used by 'jag debug --web' to map a paused (method, offset) to a
          source line. $filter-help
          """
      --rest=[snapshot-option, filter-option]
      --examples=[
        cli.Example "Print bytecode positions for snapshot 'foo.snapshot':"
            --arguments="foo.snapshot",
      ]
      --run=:: | invocation/cli.Invocation |
          with-filtered-cli-program invocation: | program/Program |
            print-positions program
  snapshot-command.add positions-command
```

- [ ] **Step 2: Add the `print-positions` function**

In `tools/toitp.toit`, next to `print-bytecodes` (~line 84), add:

```toit
print-positions program/Program:
  program.methods.do: | method/ToitMethod |
    info := program.method-info-for method.id
    path := info.error-path
    index := 0
    length := method.bytecodes.size
    while index < length:
      absolute-bci := method.absolute-bci-from-bci index
      pos/Position := info.position index
      print "$absolute-bci $path $pos.line $pos.column"
      opcode := method.bytecodes[index]
      index += BYTE-CODES[opcode].size
```

- [ ] **Step 3: Rebuild the host SDK**

Run (toit repo build, as the dev already does for the debug SDK):

```bash
cd ~/workspaceToit/toit && make build/host/sdk
```

Expected: builds without error. If `make build/host/sdk` is not the right target on this checkout, build the host SDK the same way the host-debugger SDK was built (whatever target produces `build/host/sdk/bin/toit`).

- [ ] **Step 4: Verify the command runs and capture a fixture**

```bash
cd ~/workspaceToit/jaguar
export JAG_TOIT_REPO_PATH=~/workspaceToit/toit
SDK=$JAG_TOIT_REPO_PATH/build/host/sdk
$SDK/bin/toit compile --snapshot -o /tmp/count_to.snapshot cmd/jag/commands/testdata/count_to.toit
$SDK/bin/toit tool snapshot positions /tmp/count_to.snapshot | head -40
```

Expected: lines of the form `<absolute_bci> <path> <line> <col>`, e.g. `263 .../count_to.toit 2 1`. There should be entries whose `<line>` is `8` (the `for` loop / `sum += i` body of `count-to`) — that line is the breakpoint target used by the integration test (Task 9). Save the **full** output:

```bash
$SDK/bin/toit tool snapshot positions /tmp/count_to.snapshot > /tmp/positions_dump.txt
wc -l /tmp/positions_dump.txt
```

Record (paste into Task 2's fixture) the lines for the `count-to` method (absolute_bci ≥ 285) and at least the first few `main` lines. Note the exact `<path>` string emitted — Task 2's `Locate`/`LineToAbs` tests and Task 9 use it verbatim.

- [ ] **Step 5: Commit (in the toit repo)**

```bash
cd ~/workspaceToit/toit
git add tools/toitp.toit
git commit -m "feat(snapshot): add 'positions' subcommand for jag debug --web

Emits one line per bytecode: <absolute_bci> <error_path> <line> <col>,
reusing MethodInfo.position/error-path. Used offline by jag to map a
paused (method, offset) to a source line."
```

---

## Task 2: `dbg/positions.go` — position map + lookups

**Files:**
- Create: `cmd/jag/dbg/positions.go`
- Test: `cmd/jag/dbg/positions_test.go`

**Interfaces:**
- Consumes: nothing from other tasks (pure parse). Uses `Method` (already defined in `protocol.go`, fields `EntryBci int`, `Arity int`).
- Produces:
  - `type Position struct { File string; Line int }`
  - `type PositionMap struct { ... }`
  - `func ParsePositions(dump string) PositionMap`
  - `func (PositionMap) Locate(entryBci, off int) (Position, bool)` — current line; `absolute_bci = entryBci + off`.
  - `func (PositionMap) LineToAbs(file string, line int) (abs int, ok bool)` — lowest `absolute_bci` whose position is `(file, line)`; for gutter breakpoints.
  - `func MethodForAbs(reg map[int]Method, abs int) (id, off int, ok bool)` — the method with the largest `EntryBci ≤ abs`; `off = abs - EntryBci`.

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/dbg/positions_test.go` (replace `positionsFixture` body with the real lines captured in Task 1, Step 4 — keep at least the `count-to` lines and the `(file,line)` used in assertions):

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import "testing"

// Real `toit tool snapshot positions` shape for count_to.toit (captured in
// Task 1). count-to has entry_bci 285 (HEADER-SIZE 4 over id 281); main 263.
// The real dump emits an ABSOLUTE source path for user files (and "<sdk>/..."
// for SDK files); the parser treats the path as an opaque space-free token, so
// this fixture uses a representative absolute-style path. Line 8 (the for-header)
// maps to bcis 292 (off 7) and 304 (off 19); line 9 to 298; line 6 is the
// method's fallback position for unannotated bytecodes.
const positionsFixture = `263 /proj/count_to.toit 2 1
285 /proj/count_to.toit 6 1
287 /proj/count_to.toit 6 10
292 /proj/count_to.toit 8 17
298 /proj/count_to.toit 9 9
304 /proj/count_to.toit 8 23
316 /proj/count_to.toit 10 3
`

func TestParsePositionsLocate(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	// count-to entry_bci is 285; off 7 -> absolute 292 -> line 8.
	pos, ok := pm.Locate(285, 7)
	if !ok {
		t.Fatalf("Locate(285,7) not found")
	}
	if pos.Line != 8 || pos.File != "/proj/count_to.toit" {
		t.Errorf("Locate(285,7) = %+v, want /proj/count_to.toit:8", pos)
	}
}

func TestLocateMiss(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	if _, ok := pm.Locate(285, 999); ok {
		t.Errorf("Locate of unmapped absolute bci should miss")
	}
}

func TestLineToAbsLowest(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	// Line 8 appears at absolute 292 and 304; the lowest (292) wins.
	abs, ok := pm.LineToAbs("/proj/count_to.toit", 8)
	if !ok || abs != 292 {
		t.Errorf("LineToAbs(...,8) = %d,%v, want 292,true", abs, ok)
	}
	if _, ok := pm.LineToAbs("/proj/count_to.toit", 999); ok {
		t.Errorf("LineToAbs of a line with no bytecode should miss")
	}
}

func TestMethodForAbs(t *testing.T) {
	reg := map[int]Method{
		259: {EntryBci: 263, Arity: 0}, // main
		281: {EntryBci: 285, Arity: 1}, // count-to
	}
	id, off, ok := MethodForAbs(reg, 292)
	if !ok || id != 281 || off != 7 {
		t.Errorf("MethodForAbs(292) = %d,%d,%v, want 281,7,true", id, off, ok)
	}
	// Below the lowest entry: miss.
	if _, _, ok := MethodForAbs(reg, 100); ok {
		t.Errorf("MethodForAbs below first entry should miss")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd ~/workspaceToit/jaguar && go test ./cmd/jag/dbg/ -run 'Positions|Locate|LineToAbs|MethodForAbs' -v`
Expected: FAIL — `undefined: ParsePositions` (and the other symbols).

- [ ] **Step 3: Write the implementation**

Create `cmd/jag/dbg/positions.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"strconv"
	"strings"
)

// Position is a source location: a file path (as emitted by the snapshot
// positions dump — a project-relative path or a "<sdk>/..." path) and a line.
type Position struct {
	File string
	Line int
}

// PositionMap maps absolute bytecode positions to source positions, built
// offline from `toit tool snapshot positions` (analogous to ParseBytecodes).
type PositionMap struct {
	// byAbs maps absolute_bci -> Position.
	byAbs map[int]Position
	// lowestByLine maps "file:line" -> the lowest absolute_bci on that line,
	// for resolving a gutter click to a breakpoint location.
	lowestByLine map[string]int
}

// ParsePositions parses the positions dump: one line per bytecode,
// "<absolute_bci> <file> <line> <col>". The file token may contain no spaces
// (snapshot paths do not); col is ignored. Lines that do not parse are skipped.
func ParsePositions(dump string) PositionMap {
	pm := PositionMap{byAbs: map[int]Position{}, lowestByLine: map[string]int{}}
	for _, line := range strings.Split(dump, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		abs, err1 := strconv.Atoi(fields[0])
		ln, err2 := strconv.Atoi(fields[len(fields)-2])
		if err1 != nil || err2 != nil {
			continue
		}
		// The file is everything between the abs and the trailing line+col.
		file := strings.Join(fields[1:len(fields)-2], " ")
		pm.byAbs[abs] = Position{File: file, Line: ln}
		key := file + ":" + strconv.Itoa(ln)
		if cur, ok := pm.lowestByLine[key]; !ok || abs < cur {
			pm.lowestByLine[key] = abs
		}
	}
	return pm
}

// Locate returns the source position for a paused (entryBci, off): the current
// line, where absolute_bci = entryBci + off.
func (pm PositionMap) Locate(entryBci, off int) (Position, bool) {
	pos, ok := pm.byAbs[entryBci+off]
	return pos, ok
}

// LineToAbs returns the lowest absolute_bci whose position is (file, line), for
// translating a gutter-click breakpoint to a bytecode location.
func (pm PositionMap) LineToAbs(file string, line int) (int, bool) {
	abs, ok := pm.lowestByLine[file+":"+strconv.Itoa(line)]
	return abs, ok
}

// MethodForAbs finds the method containing absolute bci abs: the one with the
// largest EntryBci <= abs. Returns its id and off = abs - EntryBci. Mirrors the
// VM's method-from-absolute-bci.
func MethodForAbs(reg map[int]Method, abs int) (id, off int, ok bool) {
	bestID, bestEntry := 0, -1
	for mid, m := range reg {
		if m.EntryBci <= abs && m.EntryBci > bestEntry {
			bestID, bestEntry = mid, m.EntryBci
		}
	}
	if bestEntry < 0 {
		return 0, 0, false
	}
	return bestID, abs - bestEntry, true
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/jag/dbg/ -run 'Positions|Locate|LineToAbs|MethodForAbs' -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/jag/dbg/positions.go cmd/jag/dbg/positions_test.go
git commit -m "feat(dbg): offline source-position map (PositionMap, Locate, LineToAbs)"
```

---

## Task 3: `dbg/session.go` — observer seam + state accessors

**Files:**
- Modify: `cmd/jag/dbg/session.go` (add fields to `Session`, hook `print`, add accessors)
- Test: `cmd/jag/dbg/session_test.go` (add tests)

**Interfaces:**
- Consumes: `Event` (`protocol.go`), the existing `Session` and its `print`/`drainUntil`/`drainOrSettle` paths.
- Produces (new exported `Session` methods used by the web driver in Task 6):
  - `func (s *Session) SetObserver(fn func(Event))`
  - `func (s *Session) LastPause() (id, off int, ok bool)`
  - `func (s *Session) LastStack() (Event, bool)`
  - `func (s *Session) Exited() bool`
  - `func (s *Session) MethodName(id int) string` (exported wrapper over `nameOf`)

- [ ] **Step 1: Write the failing test**

Add to `cmd/jag/dbg/session_test.go`:

```go
func TestObserverAndAccessors(t *testing.T) {
	s, ch, _ := methodsReady(t)
	var seen []Kind
	s.SetObserver(func(e Event) { seen = append(seen, e.Kind) })

	// Step: program prints, then pauses; the observer sees both events.
	ch.feed("tick", "dbg:paused step 281 2")
	if _, err := s.Do("s"); err != nil {
		t.Fatal(err)
	}
	id, off, ok := s.LastPause()
	if !ok || id != 281 || off != 2 {
		t.Errorf("LastPause = %d,%d,%v, want 281,2,true", id, off, ok)
	}
	if len(seen) < 2 || seen[len(seen)-1] != KindPaused {
		t.Errorf("observer should have seen app+paused, got %v", seen)
	}

	// Inspect: a stack event updates LastStack.
	ch.feed("dbg:stack off=287 r0=3266 r1=<obj>")
	if _, err := s.Do("i"); err != nil {
		t.Fatal(err)
	}
	st, ok := s.LastStack()
	if !ok || st.Regs[0] != "3266" || st.Regs[1] != "<obj>" {
		t.Errorf("LastStack = %+v,%v", st, ok)
	}

	if s.Exited() {
		t.Errorf("Exited should be false mid-session")
	}
	if s.MethodName(281) != "count-to" {
		t.Errorf("MethodName(281) = %q, want count-to", s.MethodName(281))
	}
}

func TestExitedAccessorTrueAfterProgramEnd(t *testing.T) {
	s, ch, _ := methodsReady(t)
	ch.feed("done")
	close(ch.lines)
	if _, err := s.Do("c"); err != nil {
		t.Fatal(err)
	}
	if !s.Exited() {
		t.Errorf("Exited should be true after the program ends")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/jag/dbg/ -run 'Observer|Exited' -v`
Expected: FAIL — `s.SetObserver undefined` (and the other new methods).

- [ ] **Step 3: Add fields to the `Session` struct**

In `cmd/jag/dbg/session.go`, extend the struct (after the `exited` field, line ~26):

```go
type Session struct {
	ch    Channel
	names NameMap
	out   io.Writer

	reg      map[int]Method
	resolver *Resolver
	exited   bool // the debugged program has finished and the VM is gone

	observer  func(Event) // optional structured sink (the web driver); nil for text modes
	lastPause Event        // most recent KindPaused event
	havePause bool
	lastStack Event // most recent KindStack event
	haveStack bool
}
```

- [ ] **Step 4: Hook `print` and add the accessors**

In `cmd/jag/dbg/session.go`, replace the `print` method (line ~105) with:

```go
// print pretty-prints a protocol event (or forwards app output verbatim), and
// feeds the structured observer / state accessors used by non-text drivers.
func (s *Session) print(e Event) {
	switch e.Kind {
	case KindPaused:
		s.lastPause, s.havePause = e, true
	case KindStack:
		s.lastStack, s.haveStack = e, true
	}
	if s.observer != nil {
		s.observer(e)
	}
	fmt.Fprintln(s.out, s.format(e))
}
```

Then add, just below `print` (the accessors are read by the web driver after each `Do`):

```go
// SetObserver installs a structured sink called for every parsed event, in
// addition to text output. Used by the web driver; unset in REPL/script modes.
func (s *Session) SetObserver(fn func(Event)) { s.observer = fn }

// LastPause returns the method id and offset of the most recent pause.
func (s *Session) LastPause() (id, off int, ok bool) {
	return s.lastPause.ID, s.lastPause.Off, s.havePause
}

// LastStack returns the most recent dbg:stack event (frame registers).
func (s *Session) LastStack() (Event, bool) { return s.lastStack, s.haveStack }

// Exited reports whether the debugged program has finished and the VM is gone.
func (s *Session) Exited() bool { return s.exited }

// MethodName resolves a method id to its name, or "#<id>" if unknown.
func (s *Session) MethodName(id int) string { return s.nameOf(id) }
```

- [ ] **Step 5: Run the new and existing tests to verify they pass**

Run: `go test ./cmd/jag/dbg/ -v`
Expected: PASS — the two new tests plus all existing `dbg` tests (the `print` change is behavior-preserving for text modes; `format`/output strings are unchanged).

- [ ] **Step 6: Commit**

```bash
git add cmd/jag/dbg/session.go cmd/jag/dbg/session_test.go
git commit -m "feat(dbg): observer seam + LastPause/LastStack/Exited/MethodName accessors"
```

---

## Task 4: `commands/util.go` — `SnapshotPositions` helper

**Files:**
- Modify: `cmd/jag/commands/util.go` (add the helper next to `SnapshotBytecodes`, line ~185)

**Interfaces:**
- Consumes: the `SDK` type and its `ToitPath()` (used by `SnapshotBytecodes`).
- Produces: `func (s *SDK) SnapshotPositions(ctx context.Context, snapshot string) ([]byte, error)` — runs `toit tool snapshot positions <snapshot>` and returns stdout. Consumed by `runWeb` (Task 6).

- [ ] **Step 1: Add the helper**

In `cmd/jag/commands/util.go`, directly after `SnapshotBytecodes` (ends ~line 185), add:

```go
// SnapshotPositions returns the output of `toit tool snapshot positions
// <snapshot>`: one line per bytecode, "<absolute_bci> <file> <line> <col>",
// used for offline source-position resolution (see dbg.ParsePositions). Fails
// on an SDK that lacks the subcommand (old SDK) — surfaced by --web at startup.
func (s *SDK) SnapshotPositions(ctx context.Context, snapshot string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.ToitPath(), "tool", "snapshot", "positions", snapshot)
	return cmd.Output()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/jag/...`
Expected: builds with no error (no test yet; this thin shell-out is exercised by the Task 9 integration test).

- [ ] **Step 3: Commit**

```bash
git add cmd/jag/commands/util.go
git commit -m "feat(jag): SDK.SnapshotPositions helper (toit tool snapshot positions)"
```

---

## Task 5: embedded web page (`commands/web/` + `go:embed`)

**Files:**
- Create: `cmd/jag/commands/web/index.html`
- Create: `cmd/jag/commands/web/style.css`
- Create: `cmd/jag/commands/web/app.js`
- Create: `cmd/jag/commands/debug_web_assets.go` (holds the `go:embed` var + `serveIndex` handler)
- Test: add `TestServeIndex` to `cmd/jag/commands/debug_web_test.go` (new file)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces:
  - `var webFS embed.FS` (embeds `web/*`)
  - `func serveIndex(w http.ResponseWriter, r *http.Request)` — serves `web/index.html` at `/`, and `web/app.js` / `web/style.css` as static assets. Consumed by `runWeb`'s mux in Task 6.

- [ ] **Step 1: Write `index.html`**

Create `cmd/jag/commands/web/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>jag debug</title>
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <header>
    <span id="status" class="status">connecting…</span>
    <span id="location" class="location"></span>
    <span class="controls">
      <button id="btn-continue" title="continue (c)">▶ Continue</button>
      <button id="btn-step" title="step into (s)">↳ Step</button>
      <button id="btn-over" title="step over (n)">⤵ Over</button>
      <button id="btn-out" title="step out (f)">⤴ Out</button>
    </span>
  </header>
  <main>
    <section id="source-pane">
      <div id="source" class="source">source unavailable</div>
    </section>
    <aside id="vars-pane">
      <h2>Variables</h2>
      <table id="vars"><tbody></tbody></table>
      <p id="notice" class="notice"></p>
    </aside>
  </main>
  <script src="/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Write `style.css`**

Create `cmd/jag/commands/web/style.css`:

```css
* { box-sizing: border-box; }
body { margin: 0; font-family: system-ui, sans-serif; color: #1e1e1e; }
header { display: flex; align-items: center; gap: 1rem; padding: .5rem 1rem;
  background: #2d2d30; color: #eee; position: sticky; top: 0; }
.status { font-weight: 600; }
.status.paused { color: #f1c40f; } .status.running { color: #2ecc71; }
.status.exited { color: #e74c3c; }
.location { color: #9cdcfe; font-family: monospace; }
.controls { margin-left: auto; display: flex; gap: .4rem; }
button { cursor: pointer; background: #0e639c; color: #fff; border: 0;
  padding: .35rem .7rem; border-radius: 3px; }
button:disabled { opacity: .4; cursor: default; }
main { display: flex; height: calc(100vh - 48px); }
#source-pane { flex: 1; overflow: auto; }
.source { font-family: monospace; font-size: 13px; white-space: pre;
  counter-reset: none; }
.line { display: flex; }
.gutter { width: 3.5rem; text-align: right; padding-right: .6rem; color: #858585;
  user-select: none; cursor: pointer; border-right: 1px solid #ddd; }
.gutter:hover { background: #f3d9d9; }
.line.bp .gutter { background: #e74c3c; color: #fff; }
.line.current { background: #fff3bf; }
.code { padding-left: .6rem; flex: 1; }
#vars-pane { width: 22rem; border-left: 1px solid #ddd; padding: 0 1rem;
  overflow: auto; }
#vars { width: 100%; border-collapse: collapse; font-family: monospace; }
#vars td { border-bottom: 1px solid #eee; padding: .2rem .4rem; }
.notice { color: #c0392b; min-height: 1.2em; }
```

- [ ] **Step 3: Write `app.js`**

Create `cmd/jag/commands/web/app.js`:

```javascript
// jag debug web UI: subscribes to /events (SSE), renders source + current line
// + gutter breakpoints + variables, and POSTs commands to /cmd.
let state = { status: "running", location: null, breakpoints: [], variables: [], method_id: 0 };
let sourceFile = null;
let sourceLines = [];

async function postCmd(body) {
  const notice = document.getElementById("notice");
  notice.textContent = "";
  const res = await fetch("/cmd", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) notice.textContent = await res.text();
}

function bpSet(file, line) {
  return state.breakpoints.some(b => b.file === file && b.line === line);
}

function toggleBreakpoint(line) {
  if (!sourceFile) return;
  postCmd({ verb: bpSet(sourceFile, line) ? "clear" : "break", file: sourceFile, line });
}

async function loadSource(file) {
  const res = await fetch("/source?file=" + encodeURIComponent(file));
  sourceFile = file;
  sourceLines = res.ok ? (await res.text()).split("\n") : null;
  renderSource();
}

function renderSource() {
  const el = document.getElementById("source");
  el.innerHTML = "";
  if (!sourceLines) { el.textContent = "source unavailable for " + sourceFile; return; }
  const cur = state.location && state.location.file === sourceFile ? state.location.line : -1;
  sourceLines.forEach((text, i) => {
    const n = i + 1;
    const row = document.createElement("div");
    row.className = "line" + (n === cur ? " current" : "") + (bpSet(sourceFile, n) ? " bp" : "");
    const g = document.createElement("span");
    g.className = "gutter"; g.textContent = n;
    g.onclick = () => toggleBreakpoint(n);
    const c = document.createElement("span");
    c.className = "code"; c.textContent = text;
    row.append(g, c); el.append(row);
  });
}

function renderVars() {
  const tbody = document.querySelector("#vars tbody");
  tbody.innerHTML = "";
  for (const v of state.variables) {
    const tr = document.createElement("tr");
    const k = document.createElement("td"); k.textContent = "r" + v.slot;
    const val = document.createElement("td"); val.textContent = v.value;
    tr.append(k, val); tbody.append(tr);
  }
}

function renderHeader() {
  const s = document.getElementById("status");
  s.textContent = state.status; s.className = "status " + state.status;
  const loc = document.getElementById("location");
  loc.textContent = state.location
    ? `${state.location.file}:${state.location.line}  (${state.location.method})` : "";
  const running = state.status !== "paused";
  for (const id of ["btn-continue", "btn-step", "btn-over", "btn-out"])
    document.getElementById(id).disabled = state.status === "exited" || running;
}

async function apply(update) {
  state = update;
  renderHeader(); renderVars();
  if (state.location && state.location.file !== sourceFile) await loadSource(state.location.file);
  else renderSource();
}

document.getElementById("btn-continue").onclick = () => postCmd({ verb: "continue" });
document.getElementById("btn-step").onclick = () => postCmd({ verb: "step" });
document.getElementById("btn-over").onclick = () => postCmd({ verb: "over" });
document.getElementById("btn-out").onclick = () => postCmd({ verb: "out" });

const events = new EventSource("/events");
events.onmessage = (e) => apply(JSON.parse(e.data));
```

- [ ] **Step 4: Write the embed var + `serveIndex`, and the failing test**

Create `cmd/jag/commands/debug_web_assets.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed web/*
var webFS embed.FS

// serveIndex serves the embedded single-page UI and its static assets.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	data, err := webFS.ReadFile("web" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Write(data)
}
```

Create `cmd/jag/commands/debug_web_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	serveIndex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="source"`) {
		t.Errorf("index.html should contain the source pane; got %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestServeAssetMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nope.js", nil)
	rec := httptest.NewRecorder()
	serveIndex(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./cmd/jag/commands/ -run 'ServeIndex|ServeAsset' -v`
Expected: PASS (the `go:embed` directive bundles `web/*`; both tests green).

- [ ] **Step 6: Commit**

```bash
git add cmd/jag/commands/web/ cmd/jag/commands/debug_web_assets.go cmd/jag/commands/debug_web_test.go
git commit -m "feat(jag): embedded debug web page (source/controls/vars) + serveIndex"
```

---

## Task 6: `debug_web.go` — `runWeb` driver, `StateUpdate`, breakpoint mapping, HTTP handlers

**Files:**
- Create: `cmd/jag/commands/debug_web.go`
- Test: add to `cmd/jag/commands/debug_web_test.go`

**Interfaces:**
- Consumes:
  - `dbg.PositionMap` + `ParsePositions`, `Locate`, `LineToAbs`, `MethodForAbs` (Task 2).
  - `*dbg.Session` accessors `SetObserver`, `LastPause`, `LastStack`, `Exited`, `MethodName`, `Do`, `Registry` (Task 3 + existing).
  - `dbg.Method` (`EntryBci`), `dbg.Event` (`Regs map[int]string`).
  - `sdk.SnapshotPositions` (Task 4), `serveIndex` (Task 5).
- Produces:
  - `type StateUpdate struct { ... }` (JSON shape below)
  - `type webDriver struct { ... }` with:
    - `func newWebDriver(s *dbg.Session, pm dbg.PositionMap, srcDir, sdkLib string) *webDriver`
    - `func (d *webDriver) snapshotState() StateUpdate` — builds the current `StateUpdate`.
    - `func (d *webDriver) handleCmd(c command) (StateUpdate, error)` — applies one browser command and returns the fresh state.
    - `func (d *webDriver) resolveSourcePath(file string) (string, bool)`
  - `func runWeb(ctx, sdk, entrypoint, snapshot string, session *dbg.Session, names dbg.NameMap) error` — assembles the map, starts the server, opens the browser, blocks until the program exits / the process is interrupted.

**Data model (jag → browser), serialized as JSON over SSE:**

```go
type StateUpdate struct {
	Status      string       `json:"status"` // "paused" | "running" | "exited"
	Location    *Location    `json:"location"`
	Breakpoints []Breakpoint `json:"breakpoints"`
	Variables   []Variable   `json:"variables"`
	MethodID    int          `json:"method_id"`
}
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Method string `json:"method"`
}
type Breakpoint struct {
	File string `json:"file"`
	Line int    `json:"line"`
}
type Variable struct {
	Slot  int    `json:"slot"`
	Value string `json:"value"`
}
type command struct {
	Verb string `json:"verb"` // step|over|out|continue|break|clear
	File string `json:"file"`
	Line int    `json:"line"`
}
```

- [ ] **Step 1: Write the failing test (mapping + state, no HTTP)**

Add to `cmd/jag/commands/debug_web_test.go`:

```go
import "github.com/toitlang/jaguar/cmd/jag/dbg" // add to the import block

// fakeChannel feeds scripted VM lines so we can build a real *dbg.Session
// without a VM. Mirrors dbg's test fake but lives in this package.
type webFakeChannel struct{ lines chan string }

func newWebFake() *webFakeChannel              { return &webFakeChannel{lines: make(chan string, 64)} }
func (f *webFakeChannel) Send(string) error    { return nil }
func (f *webFakeChannel) Lines() <-chan string { return f.lines }
func (f *webFakeChannel) Close() error         { return nil }
func (f *webFakeChannel) feed(ls ...string)    { for _, l := range ls { f.lines <- l } }

// webTestDriver builds a session primed with the count-to registry, paused in
// count-to, plus a position map covering count_to.toit line 8.
func webTestDriver(t *testing.T) (*webDriver, *webFakeChannel) {
	t.Helper()
	ch := newWebFake()
	names := dbg.NameMap{
		NameToEntry: map[string]int{"count-to": 285},
		EntryToName: map[int]string{285: "count-to"},
		EntrySDK:    map[int]bool{285: false},
	}
	s := dbg.NewSession(ch, names, &strings.Builder{})
	ch.feed("281 285 1", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	// count-to entry 285; line 8 (the for-header) is at bci 292 == off 7.
	pm := dbg.ParsePositions("285 count_to.toit 6 1\n292 count_to.toit 8 17\n298 count_to.toit 9 9\n")
	return newWebDriver(s, pm, ".", ""), ch
}

func TestHandleBreakMapsLineToMethodOffset(t *testing.T) {
	d, ch := webTestDriver(t)
	ch.feed("dbg:ok break")
	st, err := d.handleCmd(command{Verb: "break", File: "count_to.toit", Line: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Line 8 -> abs 287 -> count-to (entry 285) off 2 -> "dbg:break 281 2".
	if len(st.Breakpoints) != 1 || st.Breakpoints[0].Line != 8 {
		t.Errorf("breakpoint not recorded: %+v", st.Breakpoints)
	}
}

func TestHandleBreakOnDeadLineRejected(t *testing.T) {
	d, _ := webTestDriver(t)
	_, err := d.handleCmd(command{Verb: "break", File: "count_to.toit", Line: 999})
	if err == nil {
		t.Errorf("breakpoint on a line with no bytecode should be rejected")
	}
}

func TestSnapshotStatePausedLocationAndVars(t *testing.T) {
	d, ch := webTestDriver(t)
	// One step: the VM pauses at off 7 (line 8), then the driver's follow-up
	// inspect yields the frame registers. handleCmd returns the fresh state.
	ch.feed("dbg:paused step 281 7", "dbg:stack off=292 r0=3266 r1=<obj>")
	st, err := d.handleCmd(command{Verb: "step"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Location == nil || st.Location.Line != 8 || st.Location.Method != "count-to" {
		t.Errorf("location = %+v, want count_to.toit:8 count-to", st.Location)
	}
	if len(st.Variables) != 2 || st.Variables[0].Slot != 0 || st.Variables[0].Value != "3266" {
		t.Errorf("variables = %+v, want r0=3266 r1=<obj>", st.Variables)
	}
}
```

Note: `handleCmd` for a resume verb (`step`) must, after driving `session.Do`, also issue an inspect (`session.Do("i")`) so `LastStack`/variables refresh — the test feeds the `dbg:stack` line that the inspect consumes. The first `session.Do("s")` drains to the paused line and leaves the stack line buffered; the inspect then consumes it. Keep the inspect best-effort (ignore its error; a frame may be unavailable at program exit).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/jag/commands/ -run 'HandleBreak|SnapshotState' -v`
Expected: FAIL — `undefined: webDriver` / `newWebDriver` / `command`.

- [ ] **Step 3: Write `debug_web.go` (types + driver, no server yet)**

Create `cmd/jag/commands/debug_web.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

type StateUpdate struct {
	Status      string       `json:"status"`
	Location    *Location    `json:"location"`
	Breakpoints []Breakpoint `json:"breakpoints"`
	Variables   []Variable   `json:"variables"`
	MethodID    int          `json:"method_id"`
}
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Method string `json:"method"`
}
type Breakpoint struct {
	File string `json:"file"`
	Line int    `json:"line"`
}
type Variable struct {
	Slot  int    `json:"slot"`
	Value string `json:"value"`
}
type command struct {
	Verb string `json:"verb"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// webDriver translates browser commands into relay calls and snapshots session
// state into a StateUpdate. It serializes all command handling under mu (the VM
// is single-threaded; the relay is synchronous).
type webDriver struct {
	mu      sync.Mutex
	session *dbg.Session
	pm      dbg.PositionMap
	srcDir  string // directory to resolve project-relative source paths against
	sdkLib  string // SDK lib dir for "<sdk>/..." source paths ("" if unknown)
	breaks  []Breakpoint
}

func newWebDriver(s *dbg.Session, pm dbg.PositionMap, srcDir, sdkLib string) *webDriver {
	return &webDriver{session: s, pm: pm, srcDir: srcDir, sdkLib: sdkLib}
}

// handleCmd applies one browser command and returns the fresh state. Resume
// verbs drive the relay then refresh the frame; break/clear update the
// breakpoint set. Caller-facing errors (dead line, unknown verb) are returned.
func (d *webDriver) handleCmd(c command) (StateUpdate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch c.Verb {
	case "continue", "step", "over", "out":
		alias := map[string]string{"continue": "c", "step": "s", "over": "n", "out": "f"}[c.Verb]
		if _, err := d.session.Do(alias); err != nil {
			return StateUpdate{}, err
		}
		if !d.session.Exited() {
			d.session.Do("i") // refresh frame registers; best-effort
		}
	case "break", "clear":
		abs, ok := d.pm.LineToAbs(c.File, c.Line)
		if !ok {
			return StateUpdate{}, fmt.Errorf("no executable code on %s:%d", c.File, c.Line)
		}
		id, off, ok := dbg.MethodForAbs(d.session.Registry(), abs)
		if !ok {
			return StateUpdate{}, fmt.Errorf("no method covers %s:%d", c.File, c.Line)
		}
		verb := "b"
		if c.Verb == "clear" {
			verb = "d"
		}
		if _, err := d.session.Do(fmt.Sprintf("%s %d %d", verb, id, off)); err != nil {
			return StateUpdate{}, err
		}
		d.setBreak(c.Verb, c.File, c.Line)
	default:
		return StateUpdate{}, fmt.Errorf("unknown verb: %q", c.Verb)
	}
	return d.snapshotState(), nil
}

func (d *webDriver) setBreak(verb, file string, line int) {
	if verb == "clear" {
		out := d.breaks[:0]
		for _, b := range d.breaks {
			if b.File != file || b.Line != line {
				out = append(out, b)
			}
		}
		d.breaks = out
		return
	}
	for _, b := range d.breaks {
		if b.File == file && b.Line == line {
			return
		}
	}
	d.breaks = append(d.breaks, Breakpoint{File: file, Line: line})
}

// snapshotState reads the relay's current state into a StateUpdate.
func (d *webDriver) snapshotState() StateUpdate {
	st := StateUpdate{Breakpoints: append([]Breakpoint{}, d.breaks...), Variables: []Variable{}}
	if d.session.Exited() {
		st.Status = "exited"
		return st
	}
	id, off, ok := d.session.LastPause()
	if !ok {
		st.Status = "running"
		return st
	}
	st.Status = "paused"
	st.MethodID = id
	if m, found := d.session.Registry()[id]; found {
		if pos, ok := d.pm.Locate(m.EntryBci, off); ok {
			st.Location = &Location{File: pos.File, Line: pos.Line, Method: d.session.MethodName(id)}
		}
	}
	if stk, ok := d.session.LastStack(); ok {
		slots := make([]int, 0, len(stk.Regs))
		for s := range stk.Regs {
			slots = append(slots, s)
		}
		sort.Ints(slots)
		for _, s := range slots {
			st.Variables = append(st.Variables, Variable{Slot: s, Value: stk.Regs[s]})
		}
	}
	return st
}

// resolveSourcePath turns a position file into a readable disk path: a
// "<sdk>/..." path resolves against the SDK lib dir; otherwise it is tried
// as-is and then relative to the program's source directory.
func (d *webDriver) resolveSourcePath(file string) (string, bool) {
	if strings.HasPrefix(file, "<sdk>/") {
		if d.sdkLib == "" {
			return "", false
		}
		p := filepath.Join(d.sdkLib, strings.TrimPrefix(file, "<sdk>/"))
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
		return "", false
	}
	if _, err := os.Stat(file); err == nil {
		return file, true
	}
	p := filepath.Join(d.srcDir, file)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}
```

- [ ] **Step 4: Run the mapping/state tests to verify they pass**

Run: `go test ./cmd/jag/commands/ -run 'HandleBreak|SnapshotState' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing HTTP-handler test**

Add to `cmd/jag/commands/debug_web_test.go`:

```go
func TestPostCmdReturnsStateUpdate(t *testing.T) {
	d, ch := webTestDriver(t)
	srv := newWebServer(d)
	ch.feed("dbg:ok break")
	body := strings.NewReader(`{"verb":"break","file":"count_to.toit","line":8}`)
	req := httptest.NewRequest(http.MethodPost, "/cmd", body)
	rec := httptest.NewRecorder()
	srv.handleCmdHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cmd = %d (%s)", rec.Code, rec.Body.String())
	}
	var st StateUpdate
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Breakpoints) != 1 {
		t.Errorf("expected one breakpoint in StateUpdate, got %+v", st)
	}
}

func TestPostCmdRejectsDeadLine(t *testing.T) {
	d, _ := webTestDriver(t)
	srv := newWebServer(d)
	req := httptest.NewRequest(http.MethodPost, "/cmd",
		strings.NewReader(`{"verb":"break","file":"count_to.toit","line":999}`))
	rec := httptest.NewRecorder()
	srv.handleCmdHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("dead-line break = %d, want 400", rec.Code)
	}
}

func TestSourceHandlerReadsFile(t *testing.T) {
	d, _ := webTestDriver(t)
	d.srcDir = "testdata" // count_to.toit lives here
	srv := newWebServer(d)
	req := httptest.NewRequest(http.MethodGet, "/source?file=count_to.toit", nil)
	rec := httptest.NewRecorder()
	srv.handleSource(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "count-to") {
		t.Errorf("/source = %d, body=%q", rec.Code, rec.Body.String())
	}
}
```

`encoding/json` must be in the test file's import block.

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./cmd/jag/commands/ -run 'PostCmd|SourceHandler' -v`
Expected: FAIL — `undefined: newWebServer`.

- [ ] **Step 7: Add the server (handlers + SSE + runWeb) to `debug_web.go`**

Append to `cmd/jag/commands/debug_web.go`:

```go
// webServer wires the driver to HTTP: SSE state push + POST command + source.
type webServer struct {
	driver *webDriver
	mu     sync.Mutex
	subs   map[chan StateUpdate]struct{}
}

func newWebServer(d *webDriver) *webServer {
	return &webServer{driver: d, subs: map[chan StateUpdate]struct{}{}}
}

// broadcast pushes a state update to every connected SSE client.
func (s *webServer) broadcast(st StateUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- st:
		default: // slow client; drop (the next update supersedes it)
		}
	}
}

func (s *webServer) handleCmdHTTP(w http.ResponseWriter, r *http.Request) {
	var c command
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad command: "+err.Error(), http.StatusBadRequest)
		return
	}
	st, err := s.driver.handleCmd(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.broadcast(st)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

func (s *webServer) handleSource(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	path, ok := s.driver.resolveSourcePath(file)
	if !ok {
		http.Error(w, "source unavailable for "+file, http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "source unavailable for "+file, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func (s *webServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan StateUpdate, 8)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	// Push the current state immediately so a late-joining page is correct.
	sendSSE(w, flusher, s.driver.snapshotState())
	for {
		select {
		case <-r.Context().Done():
			return
		case st := <-ch:
			sendSSE(w, flusher, st)
		}
	}
}

func sendSSE(w http.ResponseWriter, f http.Flusher, st StateUpdate) {
	b, _ := json.Marshal(st)
	fmt.Fprintf(w, "data: %s\n\n", b)
	f.Flush()
}

// openBrowser best-effort opens url; failure is non-fatal (the URL is printed).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

// runWeb assembles the position map, starts the HTTP server on an ephemeral
// localhost port, opens the browser, and blocks until the program exits.
func runWeb(ctx context.Context, sdk *SDK, entrypoint, snapshot string, session *dbg.Session, names dbg.NameMap) error {
	posOut, err := sdk.SnapshotPositions(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("this SDK lacks 'snapshot positions'; rebuild the debug SDK: %w", err)
	}
	pm := dbg.ParsePositions(string(posOut))

	srcDir := filepath.Dir(entrypoint)
	sdkLib := filepath.Join(sdk.Path, "lib")
	driver := newWebDriver(session, pm, srcDir, sdkLib)
	server := newWebServer(driver)

	mux := http.NewServeMux()
	mux.HandleFunc("/cmd", server.handleCmdHTTP)
	mux.HandleFunc("/source", server.handleSource)
	mux.HandleFunc("/events", server.handleEvents)
	mux.HandleFunc("/", serveIndex)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	fmt.Printf("jag debug web UI: %s\n(program output appears below; Ctrl-C to stop)\n", url)
	openBrowser(url)

	httpServer := &http.Server{Handler: mux}
	go httpServer.Serve(ln)

	// Block until the process is interrupted. The page stays live and the VM
	// stays paused until the user stops jag; closing the session (caller's
	// channel.Close) tears down the VM.
	<-ctx.Done()
	httpServer.Close()
	return nil
}
```

- [ ] **Step 8: Run the handler tests to verify they pass**

Run: `go test ./cmd/jag/commands/ -run 'PostCmd|SourceHandler' -v`
Expected: PASS.

- [ ] **Step 9: Run the whole commands package and vet**

Run: `go test ./cmd/jag/commands/ && go vet ./cmd/jag/...`
Expected: PASS, no vet complaints.

- [ ] **Step 10: Commit**

```bash
git add cmd/jag/commands/debug_web.go cmd/jag/commands/debug_web_test.go
git commit -m "feat(jag): runWeb driver — StateUpdate, gutter breakpoints, SSE + POST handlers"
```

---

## Task 7: `debug.go` — `--web` flag, mutual exclusion, dispatch

**Files:**
- Modify: `cmd/jag/commands/debug.go` (add the flag, the mutual-exclusion check, and dispatch from `runDebug`)
- Test: `cmd/jag/commands/debug_cmd_test.go` (add flag + mutual-exclusion tests)

**Interfaces:**
- Consumes: `runWeb(ctx, sdk, entrypoint, snapshot, session, names)` (Task 6).
- Produces: `--web` bool flag on `DebugCmd`; `runDebug` gains a `web bool` parameter and dispatches to `runWeb` instead of `runREPL`/`runScript` when set.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/jag/commands/debug_cmd_test.go`:

```go
func TestDebugCmdHasWebFlag(t *testing.T) {
	cmd := DebugCmd()
	if cmd.Flags().Lookup("web") == nil {
		t.Errorf("expected --web flag")
	}
}

func TestDebugCmdRejectsWebAndScript(t *testing.T) {
	cmd := DebugCmd()
	cmd.SetArgs([]string{"-d", "host", "--web", "--script", "x.txt", "foo.toit"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--web and --script are mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}
```

Note: `TestDebugCmdRejectsWebAndScript` must fail at flag-validation time, before any file stat / SDK lookup. Place the mutual-exclusion check in `RunE` immediately after reading the flags, before the `os.Stat(entrypoint)` block — so `foo.toit` need not exist for the test.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/jag/commands/ -run 'WebFlag|WebAndScript' -v`
Expected: FAIL — no `web` flag; no mutual-exclusion error.

- [ ] **Step 3: Add the flag, the check, and dispatch**

In `cmd/jag/commands/debug.go`:

(a) Register the flag — after the `--script` flag registration (line ~71):

```go
	cmd.Flags().Bool("web", false, "serve a browser debugger UI instead of the interactive REPL")
```

(b) In `RunE`, read `--web` and enforce mutual exclusion. Replace the `scriptPath` block (lines ~58-61) with:

```go
				scriptPath, err := cmd.Flags().GetString("script")
				if err != nil {
					return err
				}
				web, err := cmd.Flags().GetBool("web")
				if err != nil {
					return err
				}
				if web && scriptPath != "" {
					return fmt.Errorf("--web and --script are mutually exclusive")
				}
```

Move this block ABOVE the `os.Stat(entrypoint)` check so the mutual-exclusion error fires before the file is touched. Then change the final call (line ~67) to:

```go
				return runDebug(ctx, sdk, entrypoint, scriptPath, web)
```

(c) Change `runDebug`'s signature and dispatch. Update the signature (line ~77):

```go
func runDebug(ctx context.Context, sdk *SDK, entrypoint, scriptPath string, web bool) error {
```

and replace the dispatch block (lines ~115-119, the `if scriptPath != "" { ... } else { ... }`) with:

```go
	if web {
		err := runWeb(ctx, sdk, entrypoint, snapshot, session, names)
		channel.Close()
		return err
	}
	if scriptPath != "" {
		runScript(session, scriptPath)
	} else {
		runREPL(session)
	}
```

(The existing `return channel.Close()` at the end of `runDebug` still covers the REPL/script paths.)

- [ ] **Step 4: Run the flag tests + full package**

Run: `go test ./cmd/jag/commands/ -v`
Expected: PASS — the two new flag tests plus all existing debug tests (the `runDebug` signature change is internal; `DebugCmd` callers are unaffected).

- [ ] **Step 5: Build the binary**

Run: `go build ./cmd/jag/...`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/jag/commands/debug.go cmd/jag/commands/debug_cmd_test.go
git commit -m "feat(jag): jag debug --web flag, mutually exclusive with --script, dispatch to runWeb"
```

---

## Task 8: gated end-to-end integration test (real VM)

**Files:**
- Create: `cmd/jag/commands/debug_web_integration_test.go`

**Interfaces:**
- Consumes: `debugCapableSDK(t)` (already in `debug_integration_test.go`), `runWeb` and the driver via the HTTP surface, `testdata/count_to.toit`. Requires the SDK to have the `snapshot positions` subcommand (Task 1) — skip if absent.

- [ ] **Step 1: Write the integration test**

Create `cmd/jag/commands/debug_web_integration_test.go`. It drives the **command path** (not a browser): compile, launch the VM, build the driver exactly as `runWeb` does, then exercise `handleCmd` to set a `(file,line)` breakpoint, continue into it, and step — asserting the `StateUpdate` location advances.

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

func TestWebDriverEndToEnd(t *testing.T) {
	sdk := debugCapableSDK(t)
	ctx := SetInfo(context.Background(), Info{})

	target, err := filepath.Abs(filepath.Join("testdata", "count_to.toit"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "prog.snapshot")
	if err := sdk.Compile(ctx, snapshot, target, -1); err != nil {
		t.Fatalf("compile: %v", err)
	}

	posOut, err := sdk.SnapshotPositions(ctx, snapshot)
	if err != nil {
		t.Skipf("SDK lacks 'snapshot positions' (rebuild debug SDK): %v", err)
	}
	pm := dbg.ParsePositions(string(posOut))

	bytecodes, err := sdk.SnapshotBytecodes(ctx, snapshot)
	if err != nil {
		t.Fatalf("bytecodes: %v", err)
	}
	names := dbg.ParseBytecodes(string(bytecodes))

	channel, err := newStdioChannel(sdk.ToitRunDebug(ctx, snapshot))
	if err != nil {
		t.Fatalf("launch VM: %v", err)
	}
	defer channel.Close()
	session := dbg.NewSession(channel, names, os.Stdout)
	if err := session.Start(); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if err := session.Methods(); err != nil {
		t.Fatalf("methods: %v", err)
	}

	driver := newWebDriver(session, pm, filepath.Dir(target), filepath.Join(sdk.Path, "lib"))

	// The positions dump emits the absolute source path for user files — exactly
	// the `target` path we compiled. Line 8 is the count-to for-header (captured
	// in Task 1: bcis 292/304). Guard in case the dump shape differs on a rebuild.
	if _, ok := pm.LineToAbs(target, 8); !ok {
		t.Skipf("no bytecode mapped to %s:8; positions dump shape differs", target)
	}

	if _, err := driver.handleCmd(command{Verb: "break", File: target, Line: 8}); err != nil {
		t.Fatalf("set breakpoint on %s:8: %v", target, err)
	}
	st, err := driver.handleCmd(command{Verb: "continue"})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if st.Status != "paused" || st.Location == nil || st.Location.Line != 8 {
		t.Fatalf("after continue, want paused at line 8, got %+v (loc=%+v)", st, st.Location)
	}

	// Step at least once and confirm we are still paused with a location.
	st, err = driver.handleCmd(command{Verb: "step"})
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if st.Status == "paused" && st.Location == nil {
		t.Errorf("paused step should carry a location, got %+v", st)
	}
}
```

- [ ] **Step 2: Run the integration test (with SDK)**

Run:

```bash
cd ~/workspaceToit/jaguar
export JAG_TOIT_REPO_PATH=~/workspaceToit/toit
go test ./cmd/jag/commands/ -run TestWebDriverEndToEnd -v
```

Expected: PASS (or SKIP if no debug-capable SDK / no `positions` dump). If it fails on the breakpoint line not being 8, adjust the target line to one captured in Task 1 (a line inside the `count-to` loop), and fix `positionFileForLine` to match the emitted path.

- [ ] **Step 3: Commit**

```bash
git add cmd/jag/commands/debug_web_integration_test.go
git commit -m "test(jag): gated end-to-end web driver test (break by file:line, continue, step)"
```

---

## Task 9: Manual smoke test + docs

**Files:**
- Modify: `docs/jag-debug.md` (document `--web`)

- [ ] **Step 1: Manual smoke test**

```bash
cd ~/workspaceToit/jaguar
export JAG_TOIT_REPO_PATH=~/workspaceToit/toit
go run ./cmd/jag debug --web cmd/jag/commands/testdata/count_to.toit
```

Confirm: the browser opens (or the URL is printed); the source shows with line numbers; clicking the gutter on line 8 sets a red breakpoint; **Continue** pauses with line 8 highlighted; **Step** advances the highlight; the variables panel shows `r0=…` slots; closing the page / Ctrl-C exits cleanly and the program output (`result=10`) appears on the console.

- [ ] **Step 2: Document `--web` in `docs/jag-debug.md`**

Read the current `docs/jag-debug.md` and add a `## Web UI` section after the REPL/`--script` documentation. Include:

```markdown
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
```

- [ ] **Step 3: Commit**

```bash
git add docs/jag-debug.md
git commit -m "docs: document jag debug --web browser UI"
```

---

## Self-Review

**1. Spec coverage:**

| Spec requirement | Task |
|---|---|
| `--web` flag, host-only, mutually exclusive with `--script` | Task 7 |
| Embedded `net/http` server on ephemeral localhost + open browser | Task 6 (`runWeb`, `openBrowser`) |
| Self-contained page: source + line numbers + current-line highlight + gutter breakpoints + controls + variables | Tasks 5 (page) + 6 (data) |
| Browser→jag commands (POST) + jag→browser push (SSE) | Task 6 (`handleCmdHTTP`, `handleEvents`) |
| Current-line resolution via offline position map | Tasks 1 (dump) + 2 (`PositionMap.Locate`) |
| `positions.go`: `PositionMap`, `ParsePositions`, `Locate` | Task 2 |
| `session.go`: observer + `LastPause`/`LastStack`/`Exited` | Task 3 |
| `debug.go`: `--web` + dispatch | Task 7 |
| `debug_web.go`: `runWeb`, handlers, browser launch | Task 6 |
| `web/{index.html,app.js,style.css}` embedded | Task 5 |
| `util.go`: `SnapshotPositions` | Task 4 |
| toit `tools` `positions` subcommand | Task 1 |
| `StateUpdate` JSON shape (status/location/breakpoints/variables/method_id) | Task 6 |
| Breakpoint (file,line)→(id,off) via lowest-offset bytecode + reject dead line | Task 6 (`handleCmd` + `MethodForAbs`) + Task 2 (`LineToAbs`) |
| Error handling (dead line, unresolved source, old SDK, program exit) | Task 6 (handlers/`runWeb`) + Task 7 (mutual exclusion) |
| Unit tests (positions, session observer, breakpoint mapping, httptest handlers) | Tasks 2, 3, 6 |
| Gated integration test (real VM) | Task 8 |
| Manual test + docs | Task 9 |
| Device symmetry (architected, not built) | Honored: transport-agnostic `dbg` core unchanged; page+`StateUpdate` host-agnostic — no task needed |

All spec sections map to a task.

**2. Placeholder scan:** The only deliberately empirical element is the position-dump fixture/path token (Task 1 captures it; Tasks 2 and 8 carry concrete representative values plus an explicit "adjust if the real path differs" instruction). This is unavoidable for a cross-repo dump whose exact path formatting is only knowable after the SDK rebuild — it is not a hidden placeholder; the capture-and-verify step is part of the plan. No "TODO/handle errors appropriately/similar to Task N" placeholders remain.

**3. Type consistency:** `StateUpdate`/`Location`/`Breakpoint`/`Variable`/`command` are defined once (Task 6) and consumed by the handlers (Task 6) and JS (Task 5, matching JSON tags `status`/`location`/`breakpoints`/`variables`/`method_id`, `file`/`line`, `slot`/`value`, `verb`/`file`/`line`). `webDriver` methods `handleCmd`/`snapshotState`/`resolveSourcePath`/`setBreak` are consistent across Tasks 6 and 8. `dbg` exports — `Position{File,Line}`, `PositionMap`, `ParsePositions`, `Locate(entryBci,off)`, `LineToAbs(file,line)`, `MethodForAbs(reg,abs)`, `SetObserver`, `LastPause`, `LastStack`, `Exited`, `MethodName` — are defined in Tasks 2/3 and used with matching signatures in Task 6/8. `runDebug` signature change (adds `web bool`) is applied at its one definition and its one call site (Task 7). `sdk.SnapshotPositions` (Task 4) matches the `SnapshotBytecodes` shape and is called in Tasks 6 and 8.
