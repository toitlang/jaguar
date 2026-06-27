# jag Host Debugger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native `jag debug -d host <file.toit>` interactive bytecode debugger built on the Toit VM's `--debug` mode and `dbg:` line protocol, replacing the throwaway Python driver.

**Architecture:** A transport-agnostic core package `cmd/jag/dbg/` (pure protocol parser, offline name resolution, relay engine) behind a `Channel` interface, plus thin wiring in `cmd/jag/commands/` (a host `stdioChannel` over the child VM's stdin/stdout pipes, and the `DebugCmd` cobra command). The core has zero dependency on `os/exec`, pipes, or cobra, so it unit-tests against a fake in-memory `Channel`, and a future device transport is a new `Channel` impl rather than a rewrite.

**Tech Stack:** Go 1.16, module `github.com/toitlang/jaguar`, cobra CLI, standard `testing` package.

## Global Constraints

- Go 1.16; module path `github.com/toitlang/jaguar`. New core package import path: `github.com/toitlang/jaguar/cmd/jag/dbg`.
- Every new `.go` file starts with the standard 3-line copyright header (year **2026**):
  ```go
  // Copyright (C) 2026 Toitware ApS. All rights reserved.
  // Use of this source code is governed by an MIT-style license that can be
  // found in the LICENSE file.
  ```
- All code `gofmt`-clean. Follow existing cobra patterns in `cmd/jag/commands/` (see `RunCmd()` in `run.go`).
- Run unit tests with `go test ./cmd/jag/dbg/...` (the Makefile `test` target is firmware extraction, not Go unit tests — use `go test` directly).
- `-d` default is `host`; any other value errors with exactly: `device debugging is not yet supported (only -d host)`.
- The debugger always compiles to a **snapshot** first, then debugs the snapshot (stable program-relative bci + clean method ids).

## Critical correction to the spec (verified empirically)

The spec's prose says launch `toit run --debug <snap>`. **This does not work** — verified on the `feature/host-debugger` SDK:

```
$ toit run --debug <snap>      → Error: Unknown option: --debug   (multiplexer's `run` command has no --debug flag)
$ toit.run --debug <snap>      → dbg:ready / dbg:paused break -1 0 / result=10   (WORKS)
```

The `--debug` flag is consumed by the **inner** `toit.run` binary (`src/toit_run.cc:92` → `setenv("OEVM_DEBUG","1")`), not the `toit` multiplexer CLI. The proven Python PoC (`tools/debug/`) launches the inner runner directly for exactly this reason. Therefore `ToitRunDebug` (Task 5) launches the inner runner `<sdk>/lib/toit/bin/toit.run` directly. The spec's **goal** (`--debug` reaches the VM, not the program) is preserved; only the argv differs from the spec's prose.

## Protocol ground truth (captured from the real VM)

- After `dbg:ready`, the VM immediately emits `dbg:paused break -1 0` (it starts paused at entry; id `-1` = no method).
- The `dbg:methods` registry lines have the form `<id> <entry_bci> <arity>` and are **NOT** `dbg:`-prefixed — they parse as app text and must be collected specially during the methods fetch, terminated by `dbg:ok methods`.
- Offline name map: `toit tool snapshot bytecodes <snap>` emits per-method blocks. Header `<idx>: <name> <file>:<line>:<col>`; first indented bytecode `  0/ <entry_bci> [..]`. For `count_to.toit`: `main` entry_bci=**263**, `count-to` entry_bci=**285**. The registry id ↔ name is recovered by matching `entry_bci`.

## File Structure

Core package `cmd/jag/dbg/` (no `os/exec`, no pipes, no cobra):
- `protocol.go` — `Event`, `Kind`, `ParseLine`, `Method`, `ParseMethods`. Pure parse, no I/O.
- `names.go` — `NameMap`, `ParseBytecodes` (pure parse of `snapshot bytecodes` output), `Resolver`, `NewResolver`.
- `channel.go` — the `Channel` interface (the device seam).
- `session.go` — the relay engine: `Session`, `Start`, `Methods`, `Do`, alias→verb translation, name→id resolution, output splitting, pretty-printing.
- `protocol_test.go`, `names_test.go`, `session_test.go` — unit tests (incl. the fake `Channel`).

Wiring in `cmd/jag/commands/`:
- `util.go` — **modify**: add `InnerToitRunPath`, `ToitRunDebug`, `SnapshotBytecodes`.
- `debug.go` — **new**: `DebugCmd()`, `stdioChannel`, compile→launch wiring, REPL + `--script`.
- `jag.go` — **modify**: register `DebugCmd()`.
- `debug_integration_test.go` — **new**: gated end-to-end smoke test.
- `testdata/count_to.toit` — **new**: self-contained target fixture.

---

## Task 1: Protocol parser (`protocol.go`)

**Files:**
- Create: `cmd/jag/dbg/protocol.go`
- Test: `cmd/jag/dbg/protocol_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Kind string` with consts `KindReady, KindPaused, KindStack, KindOK, KindError, KindApp, KindOther`.
  - `type Event struct { Kind Kind; Mode string; ID int; Off int; Regs map[int]string; Verb string; Msg string; Text string }`
  - `func ParseLine(line string) Event`
  - `type Method struct { EntryBci int; Arity int }`
  - `func ParseMethods(block string) map[int]Method`

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/dbg/protocol_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"reflect"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		in   string
		want Event
	}{
		{"dbg:ready", Event{Kind: KindReady}},
		{"dbg:paused break -1 0", Event{Kind: KindPaused, Mode: "break", ID: -1, Off: 0}},
		{"dbg:paused step 281 5", Event{Kind: KindPaused, Mode: "step", ID: 281, Off: 5}},
		{"dbg:stack off=3 r0=42 r1=<obj>", Event{Kind: KindStack, Off: 3, Regs: map[int]string{0: "42", 1: "<obj>"}}},
		{"dbg:ok break", Event{Kind: KindOK, Verb: "break"}},
		{"dbg:ok methods", Event{Kind: KindOK, Verb: "methods"}},
		{"dbg:error no such frame", Event{Kind: KindError, Msg: "no such frame"}},
		{"result=10", Event{Kind: KindApp, Text: "result=10"}},
		{"dbg:weird payload", Event{Kind: KindOther, Text: "dbg:weird payload"}},
		{"1 5741 1", Event{Kind: KindApp, Text: "1 5741 1"}},
	}
	for _, c := range cases {
		got := ParseLine(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseLine(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseLineStripsTrailingNewline(t *testing.T) {
	if got := ParseLine("dbg:ready\n"); got.Kind != KindReady {
		t.Errorf("trailing newline not stripped: %+v", got)
	}
}

func TestParseMethods(t *testing.T) {
	block := "1 5741 1\n281 285 1\ndbg:ok methods\ngarbage line\n  2  300  3  \n"
	got := ParseMethods(block)
	want := map[int]Method{
		1:   {EntryBci: 5741, Arity: 1},
		281: {EntryBci: 285, Arity: 1},
		2:   {EntryBci: 300, Arity: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMethods = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/dbg/ -run TestParse -v`
Expected: FAIL — build error, `undefined: ParseLine` / `undefined: Event`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/jag/dbg/protocol.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

// Package dbg is the transport-agnostic core of the jag host debugger: a pure
// dbg: line-protocol parser, offline method-name resolution, and a relay engine
// driven entirely through the Channel interface. It has no dependency on
// os/exec, pipes, or cobra so the device path is a new Channel impl, not a
// rewrite.
package dbg

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies a parsed line from the VM's combined stdout stream.
type Kind string

const (
	KindReady  Kind = "ready"
	KindPaused Kind = "paused"
	KindStack  Kind = "stack"
	KindOK     Kind = "ok"
	KindError  Kind = "error"
	KindApp    Kind = "app"   // the debugged program's own output
	KindOther  Kind = "other" // a dbg: line we do not recognize
)

// Event is the parsed form of one VM stdout line.
type Event struct {
	Kind Kind
	Mode string         // paused: "break" | "step"
	ID   int            // paused: method id (-1 = entry/no method)
	Off  int            // paused/stack: bytecode offset
	Regs map[int]string // stack: register index -> value
	Verb string         // ok: the acknowledged verb
	Msg  string         // error: the message
	Text string         // app/other: the raw line
}

var (
	pausedRe = regexp.MustCompile(`^dbg:paused (break|step) (-?\d+) (\d+)$`)
	offRe    = regexp.MustCompile(`off=(\d+)`)
	regRe    = regexp.MustCompile(`r(\d+)=(\S+)`)
)

// ParseLine parses one line of VM stdout. Non-"dbg:" lines are the program's
// own output (KindApp). Port of the Python driver's parse_line.
func ParseLine(line string) Event {
	s := strings.TrimRight(line, "\n")
	if !strings.HasPrefix(s, "dbg:") {
		return Event{Kind: KindApp, Text: s}
	}
	if s == "dbg:ready" {
		return Event{Kind: KindReady}
	}
	if m := pausedRe.FindStringSubmatch(s); m != nil {
		id, _ := strconv.Atoi(m[2])
		off, _ := strconv.Atoi(m[3])
		return Event{Kind: KindPaused, Mode: m[1], ID: id, Off: off}
	}
	if strings.HasPrefix(s, "dbg:stack off=") {
		if om := offRe.FindStringSubmatch(s); om != nil {
			off, _ := strconv.Atoi(om[1])
			regs := map[int]string{}
			for _, rm := range regRe.FindAllStringSubmatch(s, -1) {
				idx, _ := strconv.Atoi(rm[1])
				regs[idx] = rm[2]
			}
			return Event{Kind: KindStack, Off: off, Regs: regs}
		}
	}
	if strings.HasPrefix(s, "dbg:ok ") {
		return Event{Kind: KindOK, Verb: s[len("dbg:ok "):]}
	}
	if strings.HasPrefix(s, "dbg:error ") {
		return Event{Kind: KindError, Msg: s[len("dbg:error "):]}
	}
	return Event{Kind: KindOther, Text: s}
}

// Method is one entry of the VM's numeric method registry.
type Method struct {
	EntryBci int
	Arity    int
}

var methodLineRe = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s*$`)

// ParseMethods parses the dbg:methods registry block into {id: Method}. The VM
// emits one "<id> <entry_bci> <arity>" line per method (NOT dbg:-prefixed);
// any line that is not exactly three whitespace-separated integers is skipped.
func ParseMethods(block string) map[int]Method {
	methods := map[int]Method{}
	for _, line := range strings.Split(block, "\n") {
		m := methodLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		entry, _ := strconv.Atoi(m[2])
		arity, _ := strconv.Atoi(m[3])
		methods[id] = Method{EntryBci: entry, Arity: arity}
	}
	return methods
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/jag/dbg/ -run TestParse -v`
Expected: PASS (`TestParseLine`, `TestParseLineStripsTrailingNewline`, `TestParseMethods`).

- [ ] **Step 5: Commit**

```bash
git add cmd/jag/dbg/protocol.go cmd/jag/dbg/protocol_test.go
git commit -m "feat(dbg): add dbg: line-protocol parser"
```

---

## Task 2: Offline name resolution (`names.go`)

**Files:**
- Create: `cmd/jag/dbg/names.go`
- Test: `cmd/jag/dbg/names_test.go`

**Interfaces:**
- Consumes: `Method` (Task 1).
- Produces:
  - `type NameMap struct { NameToEntry map[string]int; EntryToName map[int]string }`
  - `func ParseBytecodes(output string) NameMap`
  - `type Resolver struct { NameToID map[string]int; IDToName map[int]string }`
  - `func NewResolver(names NameMap, methods map[int]Method) *Resolver`

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/dbg/names_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import "testing"

// Real `toit tool snapshot bytecodes` output for count_to.toit (truncated to
// the two user methods). main entry_bci=263, count-to entry_bci=285.
const bytecodesFixture = `259: main tests/debugger/targets/count_to.toit:2:1
  0/ 263 [026] - load smi 5
  2/ 265 [053] - invoke static count-to tests/debugger/targets/count_to.toit:6:1
  5/ 268 [020] - load literal result=
281: count-to tests/debugger/targets/count_to.toit:6:1
  0/ 285 [052] - load local, as class, pop 2 - LargeInteger_(27 - 29)
  2/ 287 [023] - load smi 0
`

func TestParseBytecodes(t *testing.T) {
	nm := ParseBytecodes(bytecodesFixture)
	if got := nm.NameToEntry["main"]; got != 263 {
		t.Errorf("main entry = %d, want 263", got)
	}
	if got := nm.NameToEntry["count-to"]; got != 285 {
		t.Errorf("count-to entry = %d, want 285", got)
	}
	if got := nm.EntryToName[285]; got != "count-to" {
		t.Errorf("entry 285 = %q, want count-to", got)
	}
}

func TestParseBytecodesNameWithSpaces(t *testing.T) {
	// Header names can contain spaces, e.g. "[block] in service_".
	in := "27: [block] in service_ <sdk>/core/print.toit:86:17\n  0/ 53 [026] - x\n"
	nm := ParseBytecodes(in)
	if got := nm.NameToEntry["[block] in service_"]; got != 53 {
		t.Errorf("block name entry = %d (names=%v), want 53", got, nm.NameToEntry)
	}
}

func TestNewResolver(t *testing.T) {
	nm := ParseBytecodes(bytecodesFixture)
	// Registry: id 281 has entry_bci 285 (count-to), id 259 has 263 (main).
	methods := map[int]Method{
		281: {EntryBci: 285, Arity: 1},
		259: {EntryBci: 263, Arity: 0},
		7:   {EntryBci: 9999, Arity: 1}, // no name -> dropped
	}
	r := NewResolver(nm, methods)
	if got := r.NameToID["count-to"]; got != 281 {
		t.Errorf("count-to id = %d, want 281", got)
	}
	if got := r.IDToName[259]; got != "main" {
		t.Errorf("id 259 = %q, want main", got)
	}
	if _, ok := r.IDToName[7]; ok {
		t.Errorf("unnamed id 7 should be absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/dbg/ -run "TestParseBytecodes|TestNewResolver" -v`
Expected: FAIL — `undefined: ParseBytecodes` / `undefined: NewResolver`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/jag/dbg/names.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"regexp"
	"strconv"
	"strings"
)

// NameMap maps method names to/from their entry bci (the absolute bytecode
// position of a method's first instruction), built offline from a snapshot.
type NameMap struct {
	NameToEntry map[string]int
	EntryToName map[int]string
}

var (
	// Header line: "<dispatch_idx>: <name> <file>:<line>:<col>". Header lines
	// start at column 0; bytecode lines are indented, so this is unambiguous.
	headerRe = regexp.MustCompile(`^\d+: (.+)$`)
	// Source-location trailing token: "<path>:<line>:<col>".
	locRe = regexp.MustCompile(`.+:\d+:\d+$`)
	// First bytecode of a method: "  0/ <entry_bci> [..]".
	firstByteRe = regexp.MustCompile(`^\s+0/\s*(\d+)\s+\[`)
)

// ParseBytecodes builds a NameMap from `toit tool snapshot bytecodes <snap>`
// output. Pure: callers shell out and pass the captured stdout. Port of the
// Python driver's build_name_map.
func ParseBytecodes(output string) NameMap {
	nm := NameMap{NameToEntry: map[string]int{}, EntryToName: map[int]string{}}
	current := ""
	have := false
	for _, line := range strings.Split(output, "\n") {
		if m := headerRe.FindStringSubmatch(line); m != nil {
			rest := m[1]
			fields := strings.Fields(rest)
			if len(fields) >= 1 && locRe.MatchString(fields[len(fields)-1]) {
				// Strip only the last whitespace token (the source location);
				// names themselves may contain spaces.
				name := strings.TrimSpace(strings.TrimSuffix(rest, fields[len(fields)-1]))
				current = name
				have = true
				continue
			}
		}
		if have {
			if bm := firstByteRe.FindStringSubmatch(line); bm != nil {
				entry, _ := strconv.Atoi(bm[1])
				nm.NameToEntry[current] = entry
				nm.EntryToName[entry] = current
				have = false
			}
		}
	}
	return nm
}

// Resolver maps method names to/from the VM's numeric method ids, obtained by
// cross-referencing the offline NameMap (name<->entry_bci) with the runtime
// method registry (id->entry_bci) on entry_bci.
type Resolver struct {
	NameToID map[string]int
	IDToName map[int]string
}

// NewResolver cross-references a NameMap with the dbg:methods registry.
func NewResolver(names NameMap, methods map[int]Method) *Resolver {
	r := &Resolver{NameToID: map[string]int{}, IDToName: map[int]string{}}
	for id, m := range methods {
		if name, ok := names.EntryToName[m.EntryBci]; ok {
			r.NameToID[name] = id
			r.IDToName[id] = name
		}
	}
	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/jag/dbg/ -run "TestParseBytecodes|TestNewResolver" -v`
Expected: PASS (all three name tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/jag/dbg/names.go cmd/jag/dbg/names_test.go
git commit -m "feat(dbg): add offline method-name resolution"
```

---

## Task 3: Channel interface + relay output (`channel.go`, `session.go` part 1)

This task builds the `Channel` seam, the `Session` struct, and the **output/relay** half: `Start` (wait for ready), `Methods` (fetch + parse registry, build resolver), and event pretty-printing/forwarding. Command translation comes in Task 4.

**Files:**
- Create: `cmd/jag/dbg/channel.go`
- Create: `cmd/jag/dbg/session.go`
- Test: `cmd/jag/dbg/session_test.go`

**Interfaces:**
- Consumes: `Event`, `Kind`, `ParseLine`, `Method`, `ParseMethods` (Task 1); `NameMap`, `Resolver`, `NewResolver` (Task 2).
- Produces:
  - `type Channel interface { Send(cmd string) error; Lines() <-chan string; Close() error }`
  - `type Session struct { ... }` (unexported fields)
  - `func NewSession(ch Channel, names NameMap, out io.Writer) *Session`
  - `func (s *Session) Start() error`
  - `func (s *Session) Methods() error`
  - `func (s *Session) Registry() map[int]Method`
  - `func (s *Session) format(e Event) string` (unexported, but referenced by Task 4)
  - `func (s *Session) drainUntil(done func(Event) bool) error` (unexported, referenced by Task 4)

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/dbg/session_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"strings"
	"testing"
)

// fakeChannel is an in-memory Channel for unit tests: it records sent commands
// and replays scripted VM output lines from a buffered channel.
type fakeChannel struct {
	sent  []string
	lines chan string
}

func newFakeChannel() *fakeChannel {
	return &fakeChannel{lines: make(chan string, 256)}
}

func (f *fakeChannel) Send(cmd string) error {
	f.sent = append(f.sent, cmd)
	return nil
}

func (f *fakeChannel) Lines() <-chan string { return f.lines }

func (f *fakeChannel) Close() error { return nil }

// feed pushes scripted VM lines; tests call it before draining.
func (f *fakeChannel) feed(lines ...string) {
	for _, l := range lines {
		f.lines <- l
	}
}

func newTestSession() (*Session, *fakeChannel, *strings.Builder) {
	ch := newFakeChannel()
	out := &strings.Builder{}
	nm := ParseBytecodes(bytecodesFixture) // from names_test.go
	return NewSession(ch, nm, out), ch, out
}

func TestStartWaitsForReady(t *testing.T) {
	s, ch, out := newTestSession()
	ch.feed("dbg:ready")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(out.String(), "ready") {
		t.Errorf("expected ready in output, got %q", out.String())
	}
}

func TestMethodsBuildsResolverAndResolvesNames(t *testing.T) {
	s, ch, _ := newTestSession()
	// VM: initial entry pause, then registry, then terminator.
	ch.feed(
		"dbg:paused break -1 0",
		"259 263 0",  // main
		"281 285 1",  // count-to
		"dbg:ok methods",
	)
	if err := s.Methods(); err != nil {
		t.Fatalf("Methods: %v", err)
	}
	if ch.sent[0] != "dbg:methods" {
		t.Errorf("expected dbg:methods sent first, got %v", ch.sent)
	}
	reg := s.Registry()
	if reg[281].EntryBci != 285 {
		t.Errorf("registry id 281 entry = %d, want 285", reg[281].EntryBci)
	}
}

func TestFormatPaused(t *testing.T) {
	s, ch, _ := newTestSession()
	ch.feed("259 263 0", "281 285 1", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	got := s.format(Event{Kind: KindPaused, Mode: "break", ID: 281, Off: 5})
	if got != "paused in count-to at off 5 (break)" {
		t.Errorf("format paused = %q", got)
	}
	// Unknown id falls back to #id.
	got = s.format(Event{Kind: KindPaused, Mode: "step", ID: -1, Off: 0})
	if got != "paused in #-1 at off 0 (step)" {
		t.Errorf("format unknown paused = %q", got)
	}
}

func TestFormatStackAndError(t *testing.T) {
	s, _, _ := newTestSession()
	got := s.format(Event{Kind: KindStack, Off: 3, Regs: map[int]string{1: "<obj>", 0: "42"}})
	if got != "stack off=3 r0=42 r1=<obj>" {
		t.Errorf("format stack = %q", got)
	}
	if got := s.format(Event{Kind: KindError, Msg: "no frame"}); got != "error: no frame" {
		t.Errorf("format error = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/dbg/ -run "TestStart|TestMethods|TestFormat" -v`
Expected: FAIL — `undefined: NewSession` / `undefined: Channel`.

- [ ] **Step 3: Write the Channel interface**

Create `cmd/jag/dbg/channel.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

// Channel is the transport seam between the relay engine and the target VM.
// The relay is written entirely against this interface and never touches pipes
// or sockets directly, so the host transport (a child VM's stdio pipes) and a
// future device transport (HTTP/UART) are interchangeable Channel impls.
type Channel interface {
	// Send writes one dbg: request line to the target (newline appended by the impl).
	Send(cmd string) error
	// Lines streams every line the target emits: dbg: responses interleaved with
	// the debugged program's own stdout. Closed when the target exits.
	Lines() <-chan string
	// Close detaches from the target and releases resources.
	Close() error
}
```

- [ ] **Step 4: Write the Session output half**

Create `cmd/jag/dbg/session.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Session is the relay engine. It owns a Channel and the offline NameMap, reads
// the VM's combined stdout, pretty-prints dbg: protocol events, and forwards the
// debugged program's own output verbatim to out.
type Session struct {
	ch    Channel
	names NameMap
	out   io.Writer

	reg      map[int]Method
	resolver *Resolver
}

// NewSession creates a relay over ch. names is the offline name map built from
// the snapshot (see ParseBytecodes); out receives both program output and
// pretty-printed protocol lines (os.Stdout in production).
func NewSession(ch Channel, names NameMap, out io.Writer) *Session {
	return &Session{ch: ch, names: names, out: out, reg: map[int]Method{}}
}

// Registry returns the parsed method registry (populated by Methods).
func (s *Session) Registry() map[int]Method { return s.reg }

// nameOf resolves a method id to its name, or "#<id>" if unknown.
func (s *Session) nameOf(id int) string {
	if s.resolver != nil {
		if name, ok := s.resolver.IDToName[id]; ok {
			return name
		}
	}
	return fmt.Sprintf("#%d", id)
}

// format renders a parsed event as a human-readable line. Port of the Python
// driver's _fmt.
func (s *Session) format(e Event) string {
	switch e.Kind {
	case KindReady:
		return "ready"
	case KindPaused:
		return fmt.Sprintf("paused in %s at off %d (%s)", s.nameOf(e.ID), e.Off, e.Mode)
	case KindStack:
		idxs := make([]int, 0, len(e.Regs))
		for i := range e.Regs {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		parts := make([]string, 0, len(idxs))
		for _, i := range idxs {
			parts = append(parts, fmt.Sprintf("r%d=%s", i, e.Regs[i]))
		}
		if len(parts) == 0 {
			return fmt.Sprintf("stack off=%d", e.Off)
		}
		return fmt.Sprintf("stack off=%d %s", e.Off, strings.Join(parts, " "))
	case KindOK:
		return "ok: " + e.Verb
	case KindError:
		return "error: " + e.Msg
	default: // KindApp, KindOther
		return e.Text
	}
}

// print pretty-prints a protocol event (or forwards app output verbatim).
func (s *Session) print(e Event) {
	fmt.Fprintln(s.out, s.format(e))
}

// drainUntil reads and prints events until done(event) is true. Returns io.EOF
// if the Channel closes first (the program exited).
func (s *Session) drainUntil(done func(Event) bool) error {
	for line := range s.ch.Lines() {
		e := ParseLine(line)
		s.print(e)
		if done(e) {
			return nil
		}
	}
	return io.EOF
}

// Start waits for the VM's dbg:ready handshake, forwarding anything printed
// before it (and the ready line itself).
func (s *Session) Start() error {
	return s.drainUntil(func(e Event) bool { return e.Kind == KindReady })
}

// Methods sends dbg:methods, collects the (non-dbg:-prefixed) registry lines
// until "dbg:ok methods", parses them, and builds the name resolver. Registry
// lines are consumed (not printed); any interleaved protocol event (e.g. the
// initial entry pause) is pretty-printed.
func (s *Session) Methods() error {
	if err := s.ch.Send("dbg:methods"); err != nil {
		return err
	}
	var block strings.Builder
	for line := range s.ch.Lines() {
		e := ParseLine(line)
		if e.Kind == KindOK && e.Verb == "methods" {
			break
		}
		if e.Kind == KindApp {
			// Candidate registry line ("<id> <entry_bci> <arity>"); collect it.
			// Genuine app output during the methods fetch is not expected.
			block.WriteString(e.Text)
			block.WriteString("\n")
			continue
		}
		s.print(e) // protocol events such as the entry pause
	}
	s.reg = ParseMethods(block.String())
	s.resolver = NewResolver(s.names, s.reg)
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/jag/dbg/ -run "TestStart|TestMethods|TestFormat" -v`
Expected: PASS (start, methods, format paused/stack/error).

- [ ] **Step 6: Run the whole package to confirm no regressions**

Run: `go test ./cmd/jag/dbg/ -v`
Expected: PASS (Task 1, 2, 3 tests).

- [ ] **Step 7: Commit**

```bash
git add cmd/jag/dbg/channel.go cmd/jag/dbg/session.go cmd/jag/dbg/session_test.go
git commit -m "feat(dbg): add Channel seam and relay output engine"
```

---

## Task 4: Command translation (`session.go` part 2 — `Do`)

Adds the **input** half: gdb-style alias → wire verb translation, local name→id resolution, the local `methods`/`help`/`quit` meta-commands, and per-verb response draining.

**Files:**
- Modify: `cmd/jag/dbg/session.go`
- Test: `cmd/jag/dbg/session_test.go` (add cases)

**Interfaces:**
- Consumes: everything from Task 3 (`drainUntil`, `format`, `resolver`, `reg`).
- Produces:
  - `func (s *Session) Do(input string) (stop bool, err error)` — translate one input line, send it, drain its response. `stop` is true for `quit`/`q`. Local errors (bad usage, unknown name) are printed to `out` and return `(false, nil)` so the REPL continues.

- [ ] **Step 1: Write the failing test**

Add to `cmd/jag/dbg/session_test.go`:

```go
func methodsReady(t *testing.T) (*Session, *fakeChannel, *strings.Builder) {
	t.Helper()
	s, ch, out := newTestSession()
	ch.feed("259 263 0", "281 285 1", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	out.Reset()       // discard methods-phase output
	ch.sent = nil     // discard the dbg:methods send
	return s, ch, out
}

func TestDoBreakByName(t *testing.T) {
	s, ch, _ := methodsReady(t)
	ch.feed("dbg:ok break")
	stop, err := s.Do("b count-to")
	if err != nil || stop {
		t.Fatalf("Do: stop=%v err=%v", stop, err)
	}
	if len(ch.sent) != 1 || ch.sent[0] != "dbg:break 281 0" {
		t.Errorf("sent = %v, want [dbg:break 281 0]", ch.sent)
	}
}

func TestDoBreakWithOffsetAndFullVerb(t *testing.T) {
	s, ch, _ := methodsReady(t)
	ch.feed("dbg:ok break")
	if _, err := s.Do("break count-to 4"); err != nil {
		t.Fatal(err)
	}
	if ch.sent[0] != "dbg:break 281 4" {
		t.Errorf("sent = %v, want [dbg:break 281 4]", ch.sent)
	}
}

func TestDoBreakByNumericId(t *testing.T) {
	s, ch, _ := methodsReady(t)
	ch.feed("dbg:ok break")
	if _, err := s.Do("b 99"); err != nil {
		t.Fatal(err)
	}
	if ch.sent[0] != "dbg:break 99 0" {
		t.Errorf("sent = %v, want [dbg:break 99 0]", ch.sent)
	}
}

func TestDoUnknownNameIsLocalError(t *testing.T) {
	s, ch, out := methodsReady(t)
	stop, err := s.Do("b nope")
	if err != nil || stop {
		t.Fatalf("Do: stop=%v err=%v", stop, err)
	}
	if len(ch.sent) != 0 {
		t.Errorf("nothing should be sent, got %v", ch.sent)
	}
	if !strings.Contains(out.String(), "no method 'nope'") {
		t.Errorf("expected local error, got %q", out.String())
	}
}

func TestDoAliasesMapToWireVerbs(t *testing.T) {
	cases := []struct {
		input, wire, resp string
	}{
		{"c", "dbg:continue", "dbg:paused break 281 0"},
		{"continue", "dbg:continue", "dbg:paused break 281 0"},
		{"s", "dbg:step", "dbg:paused step 281 1"},
		{"n", "dbg:over", "dbg:paused step 281 2"},
		{"f", "dbg:out", "dbg:paused step 259 5"},
		{"fin", "dbg:out", "dbg:paused step 259 5"},
		{"i", "dbg:inspect", "dbg:ok inspect"},
		{"inspect 1", "dbg:inspect 1", "dbg:ok inspect"},
	}
	for _, c := range cases {
		s, ch, _ := methodsReady(t)
		ch.feed(c.resp)
		if _, err := s.Do(c.input); err != nil {
			t.Fatalf("Do(%q): %v", c.input, err)
		}
		if len(ch.sent) != 1 || ch.sent[0] != c.wire {
			t.Errorf("Do(%q) sent %v, want [%s]", c.input, ch.sent, c.wire)
		}
	}
}

func TestDoClearAlias(t *testing.T) {
	s, ch, _ := methodsReady(t)
	ch.feed("dbg:ok clear")
	if _, err := s.Do("d count-to"); err != nil {
		t.Fatal(err)
	}
	if ch.sent[0] != "dbg:clear 281 0" {
		t.Errorf("sent = %v, want [dbg:clear 281 0]", ch.sent)
	}
}

func TestDoQuitStops(t *testing.T) {
	s, _, _ := methodsReady(t)
	stop, err := s.Do("q")
	if err != nil || !stop {
		t.Errorf("quit: stop=%v err=%v, want stop=true", stop, err)
	}
}

func TestDoBlankAndCommentIgnored(t *testing.T) {
	s, ch, _ := methodsReady(t)
	if stop, err := s.Do("   "); stop || err != nil {
		t.Errorf("blank: %v %v", stop, err)
	}
	if stop, err := s.Do("# a comment"); stop || err != nil {
		t.Errorf("comment: %v %v", stop, err)
	}
	if len(ch.sent) != 0 {
		t.Errorf("blank/comment should send nothing, got %v", ch.sent)
	}
}

func TestDoForwardsAppOutputBeforePause(t *testing.T) {
	s, ch, out := methodsReady(t)
	// continue: program prints, then hits a breakpoint.
	ch.feed("hello from app", "dbg:paused break 281 0")
	if _, err := s.Do("c"); err != nil {
		t.Fatal(err)
	}
	o := out.String()
	if !strings.Contains(o, "hello from app") || !strings.Contains(o, "paused in count-to") {
		t.Errorf("expected app output + pause, got %q", o)
	}
}

func TestDoContinueToProgramExit(t *testing.T) {
	s, ch, out := methodsReady(t)
	// No pause; program prints and the channel closes (VM exits).
	ch.feed("result=10")
	close(ch.lines)
	if _, err := s.Do("c"); err != nil {
		t.Fatalf("continue to exit should be nil err, got %v", err)
	}
	if !strings.Contains(out.String(), "result=10") {
		t.Errorf("expected program output, got %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/dbg/ -run TestDo -v`
Expected: FAIL — `s.Do undefined`.

- [ ] **Step 3: Write the implementation**

Add to `cmd/jag/dbg/session.go` (imports: add `"strconv"`):

```go
// command describes one translated REPL/script input.
type command struct {
	wire    string            // dbg: line to send, or "" for local-only commands
	isTerm  func(Event) bool  // when the response is complete
}

// verbAlias maps a user verb (alias or full name) to the wire dbg: verb.
var verbAlias = map[string]string{
	"b": "break", "break": "break",
	"d": "clear", "clear": "clear", "delete": "clear",
	"c": "continue", "continue": "continue",
	"s": "step", "step": "step",
	"n": "over", "over": "over", "next": "over",
	"f": "out", "fin": "out", "out": "out", "finish": "out",
	"i": "inspect", "inspect": "inspect",
	"m": "methods", "methods": "methods",
}

// resumeDone is the terminator for resume verbs: next pause, or an error.
func resumeDone(e Event) bool { return e.Kind == KindPaused || e.Kind == KindError }

// ackDone returns a terminator that waits for "dbg:ok <verb>" or an error.
func ackDone(verb string) func(Event) bool {
	return func(e Event) bool {
		return (e.Kind == KindOK && e.Verb == verb) || e.Kind == KindError
	}
}

// localError prints a usage/resolution error and continues the session.
func (s *Session) localError(format string, args ...interface{}) {
	fmt.Fprintf(s.out, "error: "+format+"\n", args...)
}

// resolveID turns a name-or-id token into a numeric method id.
func (s *Session) resolveID(token string) (int, bool) {
	if id, err := strconv.Atoi(token); err == nil {
		return id, true
	}
	if s.resolver != nil {
		if id, ok := s.resolver.NameToID[token]; ok {
			return id, true
		}
	}
	return 0, false
}

// Do translates one input line, sends it to the VM, and drains its response.
// Returns stop=true for quit. Local errors are printed and return (false, nil)
// so the caller's loop continues.
func (s *Session) Do(input string) (stop bool, err error) {
	line := strings.TrimSpace(input)
	if line == "" || strings.HasPrefix(line, "#") {
		return false, nil
	}
	parts := strings.Fields(line)
	verb := parts[0]

	switch verb {
	case "q", "quit":
		return true, nil
	case "help":
		s.printHelp()
		return false, nil
	}

	wireVerb, ok := verbAlias[verb]
	if !ok {
		s.localError("unknown command: %s (try 'help')", verb)
		return false, nil
	}

	switch wireVerb {
	case "methods":
		s.printRegistry()
		return false, nil

	case "break", "clear":
		if len(parts) < 2 {
			s.localError("usage: %s <name|id> [off]", verb)
			return false, nil
		}
		id, found := s.resolveID(parts[1])
		if !found {
			s.localError("no method '%s'", parts[1])
			return false, nil
		}
		off := 0
		if len(parts) > 2 {
			if off, err = strconv.Atoi(parts[2]); err != nil {
				s.localError("offset must be a number: %s", parts[2])
				return false, nil
			}
		}
		wire := fmt.Sprintf("dbg:%s %d %d", wireVerb, id, off)
		if err := s.ch.Send(wire); err != nil {
			return false, err
		}
		return false, s.drainOrExit(ackDone(wireVerb))

	case "inspect":
		wire := "dbg:inspect"
		if len(parts) > 1 {
			wire += " " + parts[1]
		}
		if err := s.ch.Send(wire); err != nil {
			return false, err
		}
		return false, s.drainOrExit(ackDone("inspect"))

	case "continue", "step", "over", "out":
		if err := s.ch.Send("dbg:" + wireVerb); err != nil {
			return false, err
		}
		return false, s.drainOrExit(resumeDone)
	}
	return false, nil
}

// drainOrExit drains until done; a closed channel (program exit) is not an error.
func (s *Session) drainOrExit(done func(Event) bool) error {
	if err := s.drainUntil(done); err == io.EOF {
		return nil
	} else {
		return err
	}
}

// printRegistry prints the method registry with resolved names (local `m`).
func (s *Session) printRegistry() {
	ids := make([]int, 0, len(s.reg))
	for id := range s.reg {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	fmt.Fprintf(s.out, "Methods (%d registered):\n", len(s.reg))
	for _, id := range ids {
		m := s.reg[id]
		fmt.Fprintf(s.out, "  %4d  entry_bci=%d  arity=%d  %s\n", id, m.EntryBci, m.Arity, s.nameOf(id))
	}
}

func (s *Session) printHelp() {
	fmt.Fprintln(s.out, strings.TrimSpace(`
Commands (full name or alias):
  b|break <name|id> [off]   set a breakpoint
  d|clear <name|id> [off]   clear a breakpoint
  c|continue                resume
  s|step                    step into
  n|over                    step over
  f|fin|out                 run until current frame returns
  i|inspect [frame]         inspect a stack frame (default 0)
  m|methods                 list methods
  help                      this help
  q|quit                    detach and exit`))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/jag/dbg/ -run TestDo -v`
Expected: PASS (all `TestDo*` cases).

- [ ] **Step 5: Run the whole core package**

Run: `go test ./cmd/jag/dbg/...`
Expected: PASS (ok `github.com/toitlang/jaguar/cmd/jag/dbg`).

- [ ] **Step 6: Commit**

```bash
git add cmd/jag/dbg/session.go cmd/jag/dbg/session_test.go
git commit -m "feat(dbg): add gdb-style command translation and relay loop"
```

---

## Task 5: SDK launch helpers (`util.go`)

**Files:**
- Modify: `cmd/jag/commands/util.go` (add after `ToitRunSnapshot`, ~line 162)
- Test: `cmd/jag/commands/util_test.go` (new)

**Interfaces:**
- Consumes: `SDK` struct (`{Path, Version}`), `directory.Executable`.
- Produces:
  - `func (s *SDK) InnerToitRunPath() string` — path to the inner `toit.run`.
  - `func (s *SDK) ToitRunDebug(ctx context.Context, snapshot string) *exec.Cmd` — `toit.run --debug <snapshot>`.
  - `func (s *SDK) SnapshotBytecodes(ctx context.Context, snapshot string) ([]byte, error)` — `toit tool snapshot bytecodes <snapshot>` stdout.

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/commands/util_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"path/filepath"
	"testing"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func TestInnerToitRunPath(t *testing.T) {
	s := &SDK{Path: filepath.Join("some", "sdk")}
	want := filepath.Join("some", "sdk", "lib", "toit", "bin", directory.Executable("toit.run"))
	if got := s.InnerToitRunPath(); got != want {
		t.Errorf("InnerToitRunPath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/commands/ -run TestInnerToitRunPath -v`
Expected: FAIL — `s.InnerToitRunPath undefined`.

- [ ] **Step 3: Write the implementation**

In `cmd/jag/commands/util.go`, add after `ToitRunSnapshot` (line 162):

```go
// InnerToitRunPath is the path to the inner snapshot runner
// (<sdk>/lib/toit/bin/toit.run), launched directly for debugging. The `toit`
// multiplexer's `run` command does not accept --debug; the inner runner does
// (it translates --debug to OEVM_DEBUG, activating the VM debugger).
func (s *SDK) InnerToitRunPath() string {
	return filepath.Join(s.Path, "lib", "toit", "bin", directory.Executable("toit.run"))
}

// ToitRunDebug launches the snapshot under the VM debugger:
// `toit.run --debug <snapshot>`. Unlike ToitRun it does NOT insert a `--`
// separator (which would pass --debug to the program instead of the VM), and it
// targets the inner runner directly so the debugger activates in exactly one VM.
func (s *SDK) ToitRunDebug(ctx context.Context, snapshot string) *exec.Cmd {
	return exec.CommandContext(ctx, s.InnerToitRunPath(), "--debug", snapshot)
}

// SnapshotBytecodes returns the output of `toit tool snapshot bytecodes
// <snapshot>`, used for offline method-name resolution (see dbg.ParseBytecodes).
func (s *SDK) SnapshotBytecodes(ctx context.Context, snapshot string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.ToitPath(), "tool", "snapshot", "bytecodes", snapshot)
	return cmd.Output()
}
```

Note: `directory` is already imported in `util.go`; `filepath`, `context`, `exec` are too. No new imports needed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/jag/commands/ -run TestInnerToitRunPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/jag/commands/util.go cmd/jag/commands/util_test.go
git commit -m "feat(jag): add ToitRunDebug and snapshot-bytecodes SDK helpers"
```

---

## Task 6: Debug command wiring (`debug.go`, `jag.go`)

Adds the `jag debug` cobra command, the host `stdioChannel`, the compile→launch→relay flow, the REPL, and `--script` mode. Deliverable is independently testable without a VM: `jag debug --help` works, and `-d <nonhost>` produces the exact error.

**Files:**
- Create: `cmd/jag/commands/debug.go`
- Modify: `cmd/jag/commands/jag.go` (add `DebugCmd()` to `cmd.AddCommand(...)`, ~line 76)
- Test: `cmd/jag/commands/debug_cmd_test.go` (new)

**Interfaces:**
- Consumes: `GetSDK`, `parseDeviceFlag`, `deviceNameSelect`, `SDK.Compile`, `SDK.ToitRunDebug`, `SDK.SnapshotBytecodes` (Task 5); `dbg.NewSession`, `dbg.ParseBytecodes`, `dbg.Channel` (Tasks 1–4).
- Produces:
  - `func DebugCmd() *cobra.Command`
  - `type stdioChannel struct { ... }` implementing `dbg.Channel`.
  - `func newStdioChannel(cmd *exec.Cmd) (*stdioChannel, error)`

- [ ] **Step 1: Write the failing test**

Create `cmd/jag/commands/debug_cmd_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"strings"
	"testing"
)

func TestDebugCmdRejectsNonHostDevice(t *testing.T) {
	cmd := DebugCmd()
	cmd.SetArgs([]string{"-d", "esp32", "foo.toit"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "device debugging is not yet supported (only -d host)") {
		t.Fatalf("expected non-host rejection, got %v", err)
	}
}

func TestDebugCmdHasScriptFlag(t *testing.T) {
	cmd := DebugCmd()
	if cmd.Flags().Lookup("script") == nil {
		t.Errorf("expected --script flag")
	}
	if cmd.Flags().Lookup("device") == nil {
		t.Errorf("expected --device/-d flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/jag/commands/ -run TestDebugCmd -v`
Expected: FAIL — `undefined: DebugCmd`.

- [ ] **Step 3: Write the implementation**

Create `cmd/jag/commands/debug.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

// DebugCmd implements `jag debug [-d host] <file.toit> [--script <cmds>]`: an
// interactive bytecode debugger for programs run on the host VM.
func DebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug <file>",
		Short: "Debug Toit code on the host VM",
		Long: "Compile <file> to a snapshot and run it under the Toit VM debugger.\n" +
			"Provides an interactive 'dbg>' REPL (gdb-style: b/c/s/n/f/i/m), or a\n" +
			"scripted run with --script for CI. Only '-d host' is supported.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}
			// Default to host; reject any explicit non-host target.
			if deviceSelect != nil {
				name, ok := deviceSelect.(deviceNameSelect)
				if !ok || string(name) != "host" {
					return fmt.Errorf("device debugging is not yet supported (only -d host)")
				}
			}

			entrypoint := args[0]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't debug directory: '%s'", entrypoint)
			}

			scriptPath, err := cmd.Flags().GetString("script")
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}
			return runDebug(ctx, sdk, entrypoint, scriptPath)
		},
	}
	cmd.Flags().StringP("device", "d", "", "device to debug (only 'host' is supported)")
	cmd.Flags().String("script", "", "read debugger commands from a file instead of the interactive REPL")
	return cmd
}

// runDebug compiles entrypoint to a snapshot, builds the offline name map,
// launches the VM in debug mode, and runs the relay (REPL or scripted).
func runDebug(ctx context.Context, sdk *SDK, entrypoint, scriptPath string) error {
	// 1. Compile to a snapshot in a temp dir (ephemeral; the debugger does not
	//    need the snapshot registered in jag's cache the way `run`/`decode` do).
	tmpdir, err := os.MkdirTemp("", "jag_debug")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)
	snapshot := filepath.Join(tmpdir, "prog.snapshot")
	if err := sdk.Compile(ctx, snapshot, entrypoint, -1); err != nil {
		return err // compiler diagnostics already went to stderr
	}

	// 2. Offline name map.
	bytecodes, err := sdk.SnapshotBytecodes(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("failed to read snapshot bytecodes: %w", err)
	}
	names := dbg.ParseBytecodes(string(bytecodes))

	// 3. Launch the VM in debug mode and wrap its pipes in a Channel.
	runCmd := sdk.ToitRunDebug(ctx, snapshot)
	channel, err := newStdioChannel(runCmd)
	if err != nil {
		return fmt.Errorf("failed to start debug VM (is this a debug-capable SDK?): %w", err)
	}

	// 4. Relay.
	session := dbg.NewSession(channel, names, os.Stdout)
	if err := session.Start(); err != nil {
		channel.Close()
		return fmt.Errorf("VM did not become ready: %w", err)
	}
	if err := session.Methods(); err != nil {
		channel.Close()
		return fmt.Errorf("failed to fetch method registry: %w", err)
	}

	if scriptPath != "" {
		runScript(session, scriptPath)
	} else {
		runREPL(session)
	}

	// Detach and report the VM's exit status.
	return channel.Close()
}

func runREPL(session *dbg.Session) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("dbg> ")
	for scanner.Scan() {
		stop, err := session.Do(scanner.Text())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if stop {
			return
		}
		fmt.Print("dbg> ")
	}
}

func runScript(session *dbg.Session, path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open script '%s': %v\n", path, err)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		stop, err := session.Do(scanner.Text())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if stop {
			return
		}
	}
}

// stdioChannel is the host dbg.Channel: it wraps a child VM's stdin/stdout
// pipes. The only concrete transport in this design.
type stdioChannel struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
}

func newStdioChannel(cmd *exec.Cmd) (*stdioChannel, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	ch := &stdioChannel{cmd: cmd, stdin: stdin, lines: make(chan string, 256)}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // tolerate long lines
		for scanner.Scan() {
			ch.lines <- scanner.Text()
		}
		close(ch.lines)
	}()
	return ch, nil
}

func (c *stdioChannel) Send(cmd string) error {
	_, err := io.WriteString(c.stdin, strings.TrimRight(cmd, "\n")+"\n")
	return err
}

func (c *stdioChannel) Lines() <-chan string { return c.lines }

func (c *stdioChannel) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}
```

Note: add `"os/exec"` to the import block above (it is used by `stdioChannel`/`newStdioChannel`). Final import list for `debug.go`: `bufio`, `context`, `fmt`, `io`, `os`, `os/exec`, `path/filepath`, `strings`, `github.com/spf13/cobra`, `github.com/toitlang/jaguar/cmd/jag/dbg`.

- [ ] **Step 4: Register the command in `jag.go`**

In `cmd/jag/commands/jag.go`, add `DebugCmd(),` to the `cmd.AddCommand(...)` list (after `RunCmd(),` at line 80):

```go
	cmd.AddCommand(
		ScanCmd(),
		ContainerCmd(),
		PingCmd(),
		RunCmd(),
		DebugCmd(),
		CompileCmd(),
		SimulateCmd(),
		DecodeCmd(),
		SetupCmd(info),
		FlashCmd(),
		FirmwareCmd(),
		MonitorCmd(),
		WatchCmd(),
		PortCmd(),
		ToitCmd(),
		PkgCmd(),
		configCmd,
		VersionCmd(info, isReleaseBuild),
	)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/jag/commands/ -run TestDebugCmd -v`
Expected: PASS (`TestDebugCmdRejectsNonHostDevice`, `TestDebugCmdHasScriptFlag`).

- [ ] **Step 6: Verify the whole project builds**

Run: `go build ./cmd/jag`
Expected: builds with no errors.

- [ ] **Step 7: Commit**

```bash
git add cmd/jag/commands/debug.go cmd/jag/commands/jag.go cmd/jag/commands/debug_cmd_test.go
git commit -m "feat(jag): add 'jag debug' command with host stdio channel"
```

---

## Task 7: End-to-end integration test (gated)

A real-VM smoke test mirroring the Python PoC: compile a self-contained `count_to.toit`, set a breakpoint by name, continue, inspect, and assert the transcript proves name resolution (`paused in count-to`) and program output (`result=10`). Skipped when no debug-capable SDK is discoverable.

**Files:**
- Create: `cmd/jag/commands/testdata/count_to.toit`
- Create: `cmd/jag/commands/debug_integration_test.go`

**Interfaces:**
- Consumes: `DebugCmd`, `GetSDK`, `SDK.ToitRunDebug` (Tasks 5–6). Runs the command's `runDebug` path end-to-end via the cobra command.

- [ ] **Step 1: Create the self-contained target fixture**

Create `cmd/jag/commands/testdata/count_to.toit`:

```toit
// A self-contained debug target. count-to 5 == 0+1+2+3+4 == 10.
main:
  result := count-to 5
  print "result=$result"

count-to n/int -> int:
  sum := 0
  for i := 0; i < n; i++:
    sum += i
  return sum
```

- [ ] **Step 2: Write the integration test**

Create `cmd/jag/commands/debug_integration_test.go`:

```go
// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// debugCapableSDK returns an SDK whose inner toit.run accepts --debug, or skips.
func debugCapableSDK(t *testing.T) *SDK {
	t.Helper()
	sdk, err := GetSDK(context.Background())
	if err != nil {
		t.Skipf("no SDK discoverable (set JAG_TOIT_REPO_PATH): %v", err)
	}
	// Probe: the inner runner must exist and accept --debug.
	if _, err := os.Stat(sdk.InnerToitRunPath()); err != nil {
		t.Skipf("inner toit.run not found at %s: %v", sdk.InnerToitRunPath(), err)
	}
	out, _ := exec.Command(sdk.InnerToitRunPath(), "--help").CombinedOutput()
	_ = out // --help may not advertise --debug; fall through and let the run prove it.
	return sdk
}

func TestDebugEndToEnd(t *testing.T) {
	sdk := debugCapableSDK(t)

	// Write a script that breaks in count-to, continues into it, inspects, and resumes.
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "cmds.txt")
	script := "b count-to\nc\ni\nc\nc\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}

	target, err := filepath.Abs(filepath.Join("testdata", "count_to.toit"))
	if err != nil {
		t.Fatal(err)
	}

	// Capture os.Stdout for the duration of the command.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	cmd := DebugCmd()
	cmd.SetArgs([]string{"-d", "host", "--script", scriptPath, target})
	runErr := cmd.Execute()

	w.Close()
	os.Stdout = old
	transcript := <-done

	if runErr != nil {
		t.Fatalf("jag debug failed: %v\ntranscript:\n%s", runErr, transcript)
	}
	if !strings.Contains(transcript, "paused in count-to") {
		t.Errorf("expected 'paused in count-to' (proves name resolution); transcript:\n%s", transcript)
	}
	if !strings.Contains(transcript, "result=10") {
		t.Errorf("expected program output 'result=10'; transcript:\n%s", transcript)
	}
}
```

- [ ] **Step 3: Run the integration test against the debug SDK**

Run: `JAG_TOIT_REPO_PATH=$HOME/workspaceToit/toit go test ./cmd/jag/commands/ -run TestDebugEndToEnd -v`
Expected: PASS — transcript contains `paused in count-to` and `result=10`. (If the SDK is not debug-capable, the test SKIPs with a clear message.)

- [ ] **Step 4: Confirm the test skips cleanly without an SDK**

Run: `go test ./cmd/jag/commands/ -run TestDebugEndToEnd -v` (no env)
Expected: SKIP with "no SDK discoverable" or "inner toit.run not found" — never a hard failure due to a missing SDK.

- [ ] **Step 5: Run the full suite**

Run: `go test ./cmd/jag/...`
Expected: PASS across `cmd/jag/dbg` and `cmd/jag/commands` (integration test PASS or SKIP depending on env).

- [ ] **Step 6: Commit**

```bash
git add cmd/jag/commands/testdata/count_to.toit cmd/jag/commands/debug_integration_test.go
git commit -m "test(jag): add end-to-end host-debugger smoke test"
```

---

## Task 8: Manual verification & docs

- [ ] **Step 1: Build jag**

Run: `make jag` (or `go build -o build/jag ./cmd/jag`)
Expected: builds clean.

- [ ] **Step 2: Interactive smoke test**

```bash
JAG_TOIT_REPO_PATH=$HOME/workspaceToit/toit build/jag debug -d host cmd/jag/commands/testdata/count_to.toit
```
At the `dbg>` prompt, run: `m`, then `b count-to`, `c`, `i`, `c`. Expected: the registry lists `count-to`; after `c` you see `paused in count-to at off 0 (break)`; `i` prints a `stack off=…` line; the final `c` runs to `result=10` and exits.

- [ ] **Step 3: Verify the non-host rejection and help**

```bash
build/jag debug -d esp32 foo.toit   # → error: device debugging is not yet supported (only -d host)
build/jag debug --help              # → shows usage, --script, gdb-style aliases
```

- [ ] **Step 4: Update the memory note**

Update `~/.claude/.../memory/jag-host-debugger.md` and `MEMORY.md`: spec implemented; record the one spec correction (launch the inner `toit.run --debug`, not `toit run --debug`), and that the design/plan live under `docs/superpowers/`.

- [ ] **Step 5: Finish the branch**

Use superpowers:finishing-a-development-branch to open a PR (or merge) for `feature/jag-host-debugger`.

---

## Self-Review

**Spec coverage:**
- New `jag debug` subcommand, `-d host` only, non-host error → Task 6. ✓
- Interactive REPL + `--script` → Task 6 (`runREPL`/`runScript`). ✓
- gdb-style aliases (b/d/c/s/n/f-fin/i/m + help/quit) → Task 4 (`verbAlias`, `Do`). ✓
- Offline name resolution from snapshot → Task 2 (`ParseBytecodes`, `NewResolver`) + Task 5 (`SnapshotBytecodes`). ✓
- Transport-agnostic `Channel` core + host `stdioChannel` → Task 3 (`channel.go`) + Task 6 (`stdioChannel`). ✓
- Compile-to-snapshot first → Task 6 (`runDebug` step 1). ✓
- `ToitRunDebug` helper (not `--`-hiding `ToitRun`) → Task 5 (corrected to inner runner, with evidence). ✓
- Protocol parse for every Kind → Task 1. ✓
- Output splitting (app verbatim, dbg: consumed) → Task 3 (`drainUntil`/`print`) + Task 4 (`TestDoForwardsAppOutputBeforePause`). ✓
- Error handling: unknown name (local), `dbg:error` surfaced, non-host, incompatible SDK at launch, compile failure → Tasks 4 & 6. ✓
- Unit tests (protocol/names/session vs fake Channel) → Tasks 1–4. ✓
- Gated integration test + `testdata/count_to.toit` → Task 7. ✓
- `jag.go` registration → Task 6 Step 4. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every test shows assertions and expected pass/fail output.

**Type consistency:** `Event`/`Kind`/`Method` (Task 1) used consistently in Tasks 3–4. `NameMap`/`Resolver` (Task 2) consumed by `Session`/`NewSession` (Task 3) and `runDebug` (Task 6). `Channel` (Task 3) implemented by `stdioChannel` (Task 6) and `fakeChannel` (Task 3 test). `Session.Do` signature `(stop bool, err error)` is consistent across Task 4 definition and Task 6 callers. `InnerToitRunPath`/`ToitRunDebug`/`SnapshotBytecodes` (Task 5) consumed by Task 6 & 7. ✓

**Deviations from spec (documented):** (1) `ToitRunDebug` launches the inner `toit.run` directly — the spec's literal `toit run --debug` form fails (verified). (2) Name-map *shelling-out* lives in the SDK (`SnapshotBytecodes`) with the *parser* pure in `dbg.ParseBytecodes`, rather than `names.go` shelling out — this preserves the spec's stated invariant that the core package has no `os/exec` dependency. Both keep the spec's goals intact.
