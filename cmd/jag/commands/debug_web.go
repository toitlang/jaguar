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
	// EntryFile is the debugged program's entrypoint source path. The page
	// shows it on first load so the user can set gutter breakpoints even while
	// the VM is paused at the runtime entry stub (method -1), which has no
	// resolvable source line and therefore no Location.
	EntryFile string `json:"entry_file"`
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
	mu        sync.Mutex
	session   *dbg.Session
	pm        dbg.PositionMap
	classes   dbg.ClassNames // class id -> name, for "<obj:N>" register values
	srcDir    string         // directory to resolve project-relative source paths against
	sdkLib    string         // SDK lib dir for "<sdk>/..." source paths ("" if unknown)
	pkgRoots  pkgRoots       // package id -> root, for "<pkg:..>/..." source paths
	entryFile string         // entrypoint source path, shown on first load (set by runWeb)
	settled   bool           // a resume ran to completion (settled on idle: no new pause, not exited)
	breaks    []Breakpoint
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
	case "continue":
		if err := d.resume("c"); err != nil {
			return StateUpdate{}, err
		}
	case "step", "over", "out":
		alias := map[string]string{"step": "s", "over": "n", "out": "f"}[c.Verb]
		// "In" (step) may descend into any frame, including the SDK. "Over"/"Out"
		// must never reveal SDK internals: when they fall off the end of the
		// user's code into the runtime harness, run on to completion instead.
		allowSDK := c.Verb == "step"
		if err := d.stepToNewLine(alias, allowSDK); err != nil {
			return StateUpdate{}, err
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

// resume issues a single VM resume (used for "continue") and records the
// resulting state. If the relay returned on idle with no new pause (and not
// exited), the program ran to completion — report "done"; issuing inspect then
// would block against the parked VM and freeze the UI under the held lock.
func (d *webDriver) resume(alias string) error {
	before := d.session.PauseGen()
	if _, err := d.session.Do(alias); err != nil {
		return err
	}
	switch {
	case d.session.Exited():
		d.settled = false
	case d.session.PauseGen() > before:
		d.settled = false
		d.session.Do("i") // refresh frame registers; best-effort
	default:
		d.settled = true
	}
	return nil
}

// stepToNewLine drives one user-level step (In/Over/Out). The VM steps one
// bytecode at a time, and most bytecodes carry the method's *declaration-line*
// position as a fallback — so a single VM step makes the marker bounce back to
// e.g. `main:`. We instead repeat the VM primitive until the pause lands on a
// genuinely new source line (or steps into/out of a call), giving "one click =
// one line". Stops early on program end, a breakpoint hit, or a safety cap.
func (d *webDriver) stepToNewLine(alias string, allowSDK bool) error {
	startLine, startMethod := d.pauseLineMethod()
	const maxSteps = 2000
	for i := 0; i < maxSteps; i++ {
		before := d.session.PauseGen()
		if _, err := d.session.Do(alias); err != nil {
			return err
		}
		if d.session.Exited() {
			d.settled = false
			return nil
		}
		if d.session.PauseGen() == before {
			d.settled = true // settled on idle: ran to completion
			return nil
		}
		d.settled = false
		if d.session.LastPauseReason() == "break" || d.atNewLine(startLine, startMethod, allowSDK) {
			d.session.Do("i") // refresh frame registers; best-effort
			return nil
		}
	}
	d.session.Do("i") // safety cap: stop where we are with a fresh frame
	return nil
}

// pauseLineMethod returns the current pause's source line and method id, the
// reference point for line stepping. Returns (0, -1) if there is no resolvable
// pause yet (e.g. the entry stub), which makes the first new stop count as a
// method change and therefore meaningful.
func (d *webDriver) pauseLineMethod() (line, method int) {
	id, off, ok := d.session.LastPause()
	if !ok {
		return 0, -1
	}
	if m, found := d.session.Registry()[id]; found {
		if pos, ok := d.pm.Locate(m.EntryBci, off); ok {
			return pos.Line, id
		}
	}
	return 0, id
}

// atNewLine reports whether the current pause is a meaningful stop for line
// stepping: a different method (stepped into or out of a call), or — within the
// same method — a source line that is neither the start line nor the method's
// declaration line (the position fallback used for filler bytecodes).
func (d *webDriver) atNewLine(startLine, startMethod int, allowSDK bool) bool {
	id, off, ok := d.session.LastPause()
	if !ok {
		return true // unknown: don't loop forever
	}
	m, found := d.session.Registry()[id]
	if !found {
		return true // entry stub / unknown method: stop
	}
	pos, ok := d.pm.Locate(m.EntryBci, off)
	if !ok {
		return false // no source for this bytecode: keep stepping
	}
	if !allowSDK && strings.HasPrefix(pos.File, "<sdk>/") {
		return false // Over/Out: don't surface SDK internals, keep stepping
	}
	if id != startMethod {
		return true // entered or returned out of a call
	}
	declLine := 0
	if hp, ok := d.pm.Locate(m.EntryBci, 0); ok {
		declLine = hp.Line
	}
	return pos.Line != startLine && pos.Line != declLine
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

// runToMain sets a one-shot breakpoint at the first line of the user's main and
// resumes to it, so the page opens already paused at the program's first
// statement instead of the runtime entry stub (method -1, no source line).
// Best-effort: if main or its first line cannot be resolved the VM is left at
// the entry stub, which the page still renders (entry source, no marker).
func (d *webDriver) runToMain(entryFile string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	mainID, ok := d.session.ResolveName("main")
	if !ok {
		return
	}
	reg := d.session.Registry()
	m, ok := reg[mainID]
	if !ok {
		return
	}
	// main's bytecode runs from its EntryBci up to the next method's EntryBci.
	hi := -1
	for _, mm := range reg {
		if mm.EntryBci > m.EntryBci && (hi < 0 || mm.EntryBci < hi) {
			hi = mm.EntryBci
		}
	}
	// Prefer the first statement past main's signature line (more useful: real
	// code, locals populated). Fall back to the declaration line for a one-liner
	// main whose body shares the signature line.
	line, ok := d.pm.FirstLineInRange(entryFile, m.EntryBci, hi, 0)
	if !ok {
		return
	}
	if stmt, ok := d.pm.FirstLineInRange(entryFile, m.EntryBci, hi, line); ok {
		line = stmt
	}
	abs, ok := d.pm.LineToAbs(entryFile, line)
	if !ok {
		return
	}
	id, off, ok := dbg.MethodForAbs(reg, abs)
	if !ok {
		return
	}
	// Set the breakpoint, continue to it, then clear it (one-shot) — clearing a
	// breakpoint does not resume, so the VM stays paused at main. Don't record it
	// in d.breaks: it's internal, not a user breakpoint.
	if _, err := d.session.Do(fmt.Sprintf("b %d %d", id, off)); err != nil {
		return
	}
	if _, err := d.session.Do("c"); err != nil {
		return
	}
	d.session.Do(fmt.Sprintf("d %d %d", id, off))
}

// SnapshotState returns the current state under the driver lock, for callers
// (the SSE initial push) that are not already holding d.mu via handleCmd.
func (d *webDriver) SnapshotState() StateUpdate {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshotState()
}

// snapshotState reads the relay's current state into a StateUpdate.
func (d *webDriver) snapshotState() StateUpdate {
	st := StateUpdate{Breakpoints: append([]Breakpoint{}, d.breaks...), Variables: []Variable{}, EntryFile: d.entryFile}
	if d.session.Exited() {
		st.Status = "exited"
		return st
	}
	if d.settled {
		st.Status = "done"
		return st
	}
	id, off, ok := d.session.LastPause()
	if !ok {
		st.Status = "done"
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
			st.Variables = append(st.Variables, Variable{Slot: s, Value: d.classes.Resolve(stk.Regs[s])})
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
	if strings.HasPrefix(file, "<pkg:") {
		if p, ok := pkgFilePath(file, d.pkgRoots); ok {
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
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
	sendSSE(w, flusher, s.driver.SnapshotState())
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

	// Class-name resolution for heap-object register values ("<obj:N>"). Same SDK
	// vintage as the positions dump, so a failure here means the same old-SDK
	// problem; surface it the same way.
	classOut, err := sdk.SnapshotClassNames(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("this SDK lacks 'snapshot class-names'; rebuild the debug SDK: %w", err)
	}
	classes := dbg.ParseClassNames(string(classOut))

	srcDir := filepath.Dir(entrypoint)
	sdkLib := filepath.Join(sdk.Path, "lib")
	driver := newWebDriver(session, pm, srcDir, sdkLib)
	driver.classes = classes
	// Show the entrypoint source on first load. The entrypoint path as passed to
	// jag matches the position-map file token (both come from the compiler's
	// view of the file), so gutter clicks on it resolve via LineToAbs.
	driver.entryFile = entrypoint
	// Package source ("<pkg:..>/...") resolves via the entrypoint's package.lock,
	// so stepping In to a package method shows its source instead of the banner.
	driver.pkgRoots = loadPkgRoots(srcDir)
	// Resume to the first line of main so the page opens paused at the user's code
	// rather than the runtime entry stub. Best-effort (see runToMain).
	driver.runToMain(entrypoint)
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
