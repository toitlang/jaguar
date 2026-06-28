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
	srcDir    string // directory to resolve project-relative source paths against
	sdkLib    string // SDK lib dir for "<sdk>/..." source paths ("" if unknown)
	entryFile string // entrypoint source path, shown on first load (set by runWeb)
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

	srcDir := filepath.Dir(entrypoint)
	sdkLib := filepath.Join(sdk.Path, "lib")
	driver := newWebDriver(session, pm, srcDir, sdkLib)
	// Show the entrypoint source on first load. The entrypoint path as passed to
	// jag matches the position-map file token (both come from the compiler's
	// view of the file), so gutter clicks on it resolve via LineToAbs.
	driver.entryFile = entrypoint
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
