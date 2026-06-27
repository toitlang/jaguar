// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
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
	exited   bool // the debugged program has finished and the VM is gone

	observer  func(Event) // optional structured sink (the web driver); nil for text modes
	lastPause Event       // most recent KindPaused event
	havePause bool
	lastStack Event // most recent KindStack event
	haveStack bool
}

// NewSession creates a relay over ch. names is the offline name map built from
// the snapshot (see ParseBytecodes); out receives both program output and
// pretty-printed protocol lines (os.Stdout in production).
func NewSession(ch Channel, names NameMap, out io.Writer) *Session {
	return &Session{ch: ch, names: names, out: out, reg: map[int]Method{}}
}

// Registry returns the parsed method registry (populated by Methods).
func (s *Session) Registry() map[int]Method { return s.reg }

// markExited records that the program finished (the VM exited / its stream
// closed) and announces it once, so further commands end the session cleanly
// instead of failing against a closed pipe.
func (s *Session) markExited() {
	if !s.exited {
		s.exited = true
		fmt.Fprintln(s.out, "program exited")
	}
}

// send writes a wire command, treating a write failure — the VM has exited and
// closed its stdin — as program exit rather than a raw broken-pipe error.
// Returns true if the program has exited (caller should stop).
func (s *Session) send(wire string) (exited bool) {
	if s.exited {
		return true
	}
	if err := s.ch.Send(wire); err != nil {
		s.markExited()
		return true
	}
	return false
}

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
	s.markExited()
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
		showAll := len(parts) > 1 && parts[1] == "all"
		s.printRegistry(showAll)
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
		if s.send(wire) {
			return true, nil
		}
		err := s.drainOrExit(ackDone(wireVerb))
		return s.exited, err

	case "inspect":
		wire := "dbg:inspect"
		if len(parts) > 1 {
			wire += " " + parts[1]
		}
		if s.send(wire) {
			return true, nil
		}
		inspectDone := func(e Event) bool {
			return e.Kind == KindStack || e.Kind == KindError
		}
		err := s.drainOrExit(inspectDone)
		return s.exited, err

	case "continue", "step", "over", "out":
		if s.send("dbg:" + wireVerb) {
			return true, nil
		}
		err := s.drainOrSettle(resumeDone)
		return s.exited, err
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

// settleTimeout is how long drainOrSettle waits after the last VM output before
// treating the channel as quiescent (program completed without hitting another
// breakpoint). Mirrors the Python PoC's reader.settle() approach.
const settleTimeout = 600 * time.Millisecond

// drainOrSettle drains events until done, EOF, or the channel is idle for
// settleTimeout. The idle path handles programs that run to completion after a
// resume command: the Toit VM does not exit on program end, so no EOF ever
// arrives; the output simply stops. Returning on idle lets the caller close the
// channel (closing VM stdin) which causes the VM to exit cleanly.
func (s *Session) drainOrSettle(done func(Event) bool) error {
	timer := time.NewTimer(settleTimeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-s.ch.Lines():
			if !ok {
				s.markExited() // EOF: VM exited
				return nil
			}
			e := ParseLine(line)
			s.print(e)
			if done(e) {
				return nil
			}
			// Got output; reset the idle timer so we keep draining.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(settleTimeout)
		case <-timer.C:
			return nil // Idle for settleTimeout: program ran to completion
		}
	}
}

// isUserMethod reports whether method id belongs to the user's program (it
// resolves to a name and is not defined in the SDK). The registry contains the
// whole image — hundreds of SDK methods — so `m` shows only these by default.
func (s *Session) isUserMethod(id int) bool {
	if s.resolver == nil {
		return false
	}
	if _, named := s.resolver.IDToName[id]; !named {
		return false
	}
	return !s.names.EntrySDK[s.reg[id].EntryBci]
}

// printRegistry prints the method registry with resolved names (local `m`). By
// default it lists only the user's own methods; showAll (`m all`) lists every
// method in the image, including the SDK.
func (s *Session) printRegistry(showAll bool) {
	ids := make([]int, 0, len(s.reg))
	for id := range s.reg {
		if showAll || s.isUserMethod(id) {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	if showAll {
		fmt.Fprintf(s.out, "Methods (%d registered):\n", len(s.reg))
	} else {
		fmt.Fprintf(s.out, "Your methods (%d of %d; 'm all' for every method incl. SDK):\n", len(ids), len(s.reg))
	}
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
  m|methods [all]           list your methods (m all: include SDK)
  help                      this help
  q|quit                    detach and exit`))
}
