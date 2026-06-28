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
