// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

// fakeChannel feeds scripted VM lines so we can build a real *dbg.Session
// without a VM. Mirrors dbg's test fake but lives in this package.
type webFakeChannel struct{ lines chan string }

func newWebFake() *webFakeChannel              { return &webFakeChannel{lines: make(chan string, 64)} }
func (f *webFakeChannel) Send(string) error    { return nil }
func (f *webFakeChannel) Lines() <-chan string { return f.lines }
func (f *webFakeChannel) Close() error         { return nil }
func (f *webFakeChannel) feed(ls ...string) {
	for _, l := range ls {
		f.lines <- l
	}
}

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

// scriptedChannel models the VM's request→response timing: each resume command
// (continue/step/over/out) yields exactly one queued pause line, and each
// inspect yields one queued stack line. This is faithful to the real VM (which
// only emits a pause per resume) and lets us exercise the line-stepping loop,
// which issues several VM steps for one user click.
type scriptedChannel struct {
	lines  chan string
	pauses []string
	pi     int
	stacks []string
	si     int
}

func newScriptedChannel(pauses, stacks []string) *scriptedChannel {
	return &scriptedChannel{lines: make(chan string, 64), pauses: pauses, stacks: stacks}
}
func (c *scriptedChannel) feedRaw(ls ...string) {
	for _, l := range ls {
		c.lines <- l
	}
}
func (c *scriptedChannel) Send(cmd string) error {
	switch {
	case strings.HasPrefix(cmd, "dbg:continue"), strings.HasPrefix(cmd, "dbg:step"),
		strings.HasPrefix(cmd, "dbg:over"), strings.HasPrefix(cmd, "dbg:out"):
		if c.pi < len(c.pauses) {
			c.lines <- c.pauses[c.pi]
			c.pi++
		}
	case strings.HasPrefix(cmd, "dbg:inspect"):
		if c.si < len(c.stacks) {
			c.lines <- c.stacks[c.si]
			c.si++
		}
	}
	return nil
}
func (c *scriptedChannel) Lines() <-chan string { return c.lines }
func (c *scriptedChannel) Close() error         { return nil }

// A line-granularity Over must skip stops whose bytecode falls back to the
// method's declaration line (the position fallback that made the marker "bounce"
// to `main:`) and the start line itself, stopping at the next genuine source
// line.
func TestOverAdvancesByLineSkippingDeclarationFallback(t *testing.T) {
	ch := newScriptedChannel(
		[]string{
			"dbg:paused break 1 2", // prime: off 2 -> line 32
			"dbg:paused step 1 4",  // off 4 -> line 31 (declaration fallback)
			"dbg:paused step 1 6",  // off 6 -> line 31 (fallback)
			"dbg:paused step 1 8",  // off 8 -> line 33 (genuine)
		},
		[]string{
			"dbg:stack off=880 r0=<obj>",
			"dbg:stack off=886 r0=<obj>",
		},
	)
	names := dbg.NameMap{
		NameToEntry: map[string]int{"main": 878},
		EntryToName: map[int]string{878: "main"},
		EntrySDK:    map[int]bool{878: false},
	}
	s := dbg.NewSession(ch, names, &strings.Builder{})
	ch.feedRaw("1 878 0", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	pm := dbg.ParsePositions(
		"878 simple.toit 31 1\n" + // entry / declaration line
			"880 simple.toit 32 5\n" + // off 2
			"882 simple.toit 31 1\n" + // off 4 fallback
			"884 simple.toit 31 1\n" + // off 6 fallback
			"886 simple.toit 33 5\n") // off 8 genuine
	d := newWebDriver(s, pm, ".", "")

	if _, err := d.handleCmd(command{Verb: "continue"}); err != nil {
		t.Fatal(err) // prime: land on line 32
	}
	st, err := d.handleCmd(command{Verb: "over"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Location == nil || st.Location.Line != 33 {
		t.Errorf("after Over want line 33 (skipping the line-31 fallback bounce), got %+v", st.Location)
	}
}

// Over/Out must not reveal SDK internals: stepping off the end of the user's
// code (main returns into the runtime entry harness) should run on to
// completion ("done"), not park the marker in an <sdk>/ frame.
func TestOverPastLastUserLineRunsToDoneNotSDK(t *testing.T) {
	ch := newScriptedChannel(
		[]string{
			"dbg:paused break 1 38", // prime: off 38 -> line 35 (last user line)
			"dbg:paused step 2 0",   // returned into __entry__main (SDK)
			// no further pause: the program then settles to completion
		},
		// Two stacks: the second only gets consumed by the pre-fix code, which
		// wrongly parks in the SDK frame and then inspects it. The fixed code
		// skips the SDK frame and settles to "done" without inspecting.
		[]string{"dbg:stack off=916 r0=<obj>", "dbg:stack off=930 r0=<obj>"},
	)
	names := dbg.NameMap{
		NameToEntry: map[string]int{"main": 878, "__entry__main": 930},
		EntryToName: map[int]string{878: "main", 930: "__entry__main"},
		EntrySDK:    map[int]bool{878: false, 930: true},
	}
	s := dbg.NewSession(ch, names, &strings.Builder{})
	ch.feedRaw("1 878 0", "2 930 0", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	pm := dbg.ParsePositions(
		"878 simple.toit 31 1\n" +
			"916 simple.toit 35 3\n" + // off 38 -> last user line
			"930 <sdk>/core/entry.toit 10 1\n") // returned-into SDK frame
	d := newWebDriver(s, pm, ".", "")

	if _, err := d.handleCmd(command{Verb: "continue"}); err != nil {
		t.Fatal(err) // prime: line 35
	}
	st, err := d.handleCmd(command{Verb: "over"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Errorf("Over off the end of user code: status = %q (loc %+v), want done", st.Status, st.Location)
	}
}

func TestHandleBreakMapsLineToMethodOffset(t *testing.T) {
	d, ch := webTestDriver(t)
	ch.feed("dbg:ok break")
	st, err := d.handleCmd(command{Verb: "break", File: "count_to.toit", Line: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Line 8 -> abs 292 -> count-to (entry 285) off 7 -> "dbg:break 281 7".
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

// At the entry stub the VM is paused in method -1 (no registry entry, no source
// line) so Location is nil — but the state must still carry EntryFile so the
// page can show source and let the user set gutter breakpoints from first load.
func TestSnapshotStateCarriesEntryFileWithoutLocation(t *testing.T) {
	d, ch := webTestDriver(t)
	d.entryFile = "count_to.toit"
	// Pause at the entry stub (method -1), then a stack line for the driver's
	// follow-up inspect so handleCmd does not block.
	ch.feed("dbg:paused break -1 0", "dbg:stack off=0")
	if _, err := d.handleCmd(command{Verb: "continue"}); err != nil {
		t.Fatal(err)
	}
	st := d.snapshotState()
	if st.Status != "paused" || st.Location != nil {
		t.Errorf("at entry stub want paused with nil Location, got status=%q loc=%+v", st.Status, st.Location)
	}
	if st.EntryFile != "count_to.toit" {
		t.Errorf("EntryFile = %q, want count_to.toit", st.EntryFile)
	}
}

// A resume that settles on idle (no new pause, not exited) means the program
// ran to completion. It must NOT trigger a follow-up inspect — issuing inspect
// against the parked VM would block forever under the held driver lock and
// freeze the UI. The state is reported as "done". (Under the pre-fix code this
// test would hang.)
func TestResumeSettlingReportsDoneWithoutInspecting(t *testing.T) {
	d, ch := webTestDriver(t)
	// continue: the program prints but never pauses; the relay returns on idle.
	ch.feed("still working")
	st, err := d.handleCmd(command{Verb: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Errorf("status = %q, want done (resume settled to completion)", st.Status)
	}
}

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
	// Embedded assets change every jag build; they must not be browser-cached or
	// a rebuilt jag would serve stale front-end code.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
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
