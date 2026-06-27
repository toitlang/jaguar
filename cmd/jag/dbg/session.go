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
