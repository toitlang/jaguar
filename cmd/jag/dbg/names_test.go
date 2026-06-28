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
	// count-to/main are in the user's file, not the SDK.
	if nm.EntrySDK[285] {
		t.Errorf("count-to (285) should not be marked SDK")
	}
}

func TestParseBytecodesNameWithSpaces(t *testing.T) {
	// Header names can contain spaces, e.g. "[block] in service_".
	in := "27: [block] in service_ <sdk>/core/print.toit:86:17\n  0/ 53 [026] - x\n"
	nm := ParseBytecodes(in)
	if got := nm.NameToEntry["[block] in service_"]; got != 53 {
		t.Errorf("block name entry = %d (names=%v), want 53", got, nm.NameToEntry)
	}
	// The "<sdk>/..." source path marks this as an SDK method.
	if !nm.EntrySDK[53] {
		t.Errorf("entry 53 with <sdk> path should be marked SDK")
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
