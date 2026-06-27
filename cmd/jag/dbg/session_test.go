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
