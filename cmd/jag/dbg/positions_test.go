// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import "testing"

// Real `toit tool snapshot positions` shape for count_to.toit (captured in
// Task 1). count-to has entry_bci 285 (HEADER-SIZE 4 over id 281); main 263.
// The real dump emits an ABSOLUTE source path for user files (and "<sdk>/..."
// for SDK files); the parser treats the path as an opaque space-free token, so
// this fixture uses a representative absolute-style path. Line 8 (the for-header)
// maps to bcis 292 (off 7) and 304 (off 19); line 9 to 298; line 6 is the
// method's fallback position for unannotated bytecodes.
const positionsFixture = `263 /proj/count_to.toit 2 1
285 /proj/count_to.toit 6 1
287 /proj/count_to.toit 6 10
292 /proj/count_to.toit 8 17
298 /proj/count_to.toit 9 9
304 /proj/count_to.toit 8 23
316 /proj/count_to.toit 10 3
`

func TestParsePositionsLocate(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	// count-to entry_bci is 285; off 7 -> absolute 292 -> line 8.
	pos, ok := pm.Locate(285, 7)
	if !ok {
		t.Fatalf("Locate(285,7) not found")
	}
	if pos.Line != 8 || pos.File != "/proj/count_to.toit" {
		t.Errorf("Locate(285,7) = %+v, want /proj/count_to.toit:8", pos)
	}
}

func TestLocateMiss(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	if _, ok := pm.Locate(285, 999); ok {
		t.Errorf("Locate of unmapped absolute bci should miss")
	}
}

func TestLineToAbsLowest(t *testing.T) {
	pm := ParsePositions(positionsFixture)
	// Line 8 appears at absolute 292 and 304; the lowest (292) wins.
	abs, ok := pm.LineToAbs("/proj/count_to.toit", 8)
	if !ok || abs != 292 {
		t.Errorf("LineToAbs(...,8) = %d,%v, want 292,true", abs, ok)
	}
	if _, ok := pm.LineToAbs("/proj/count_to.toit", 999); ok {
		t.Errorf("LineToAbs of a line with no bytecode should miss")
	}
}

func TestMethodForAbs(t *testing.T) {
	reg := map[int]Method{
		259: {EntryBci: 263, Arity: 0}, // main
		281: {EntryBci: 285, Arity: 1}, // count-to
	}
	id, off, ok := MethodForAbs(reg, 292)
	if !ok || id != 281 || off != 7 {
		t.Errorf("MethodForAbs(292) = %d,%d,%v, want 281,7,true", id, off, ok)
	}
	// Below the lowest entry: miss.
	if _, _, ok := MethodForAbs(reg, 100); ok {
		t.Errorf("MethodForAbs below first entry should miss")
	}
}
