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
		"259 263 0", // main
		"281 285 1", // count-to
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

func methodsReady(t *testing.T) (*Session, *fakeChannel, *strings.Builder) {
	t.Helper()
	s, ch, out := newTestSession()
	ch.feed("259 263 0", "281 285 1", "dbg:ok methods")
	if err := s.Methods(); err != nil {
		t.Fatal(err)
	}
	out.Reset()   // discard methods-phase output
	ch.sent = nil // discard the dbg:methods send
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
		{"i", "dbg:inspect", "dbg:stack off=285"},
		{"inspect 1", "dbg:inspect 1", "dbg:stack off=285"},
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
