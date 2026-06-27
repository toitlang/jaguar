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
