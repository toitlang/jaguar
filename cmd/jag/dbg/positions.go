// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"strconv"
	"strings"
)

// Position is a source location: a file path (as emitted by the snapshot
// positions dump — a project-relative path or a "<sdk>/..." path) and a line.
type Position struct {
	File string
	Line int
}

// PositionMap maps absolute bytecode positions to source positions, built
// offline from `toit tool snapshot positions` (analogous to ParseBytecodes).
type PositionMap struct {
	// byAbs maps absolute_bci -> Position.
	byAbs map[int]Position
	// lowestByLine maps "file:line" -> the lowest absolute_bci on that line,
	// for resolving a gutter click to a breakpoint location.
	lowestByLine map[string]int
}

// ParsePositions parses the positions dump: one line per bytecode,
// "<absolute_bci> <file> <line> <col>". The file token may contain no spaces
// (snapshot paths do not); col is ignored. Lines that do not parse are skipped.
func ParsePositions(dump string) PositionMap {
	pm := PositionMap{byAbs: map[int]Position{}, lowestByLine: map[string]int{}}
	for _, line := range strings.Split(dump, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		abs, err1 := strconv.Atoi(fields[0])
		ln, err2 := strconv.Atoi(fields[len(fields)-2])
		if err1 != nil || err2 != nil {
			continue
		}
		// The file is everything between the abs and the trailing line+col.
		file := strings.Join(fields[1:len(fields)-2], " ")
		pm.byAbs[abs] = Position{File: file, Line: ln}
		key := file + ":" + strconv.Itoa(ln)
		if cur, ok := pm.lowestByLine[key]; !ok || abs < cur {
			pm.lowestByLine[key] = abs
		}
	}
	return pm
}

// Locate returns the source position for a paused (entryBci, off): the current
// line, where absolute_bci = entryBci + off.
func (pm PositionMap) Locate(entryBci, off int) (Position, bool) {
	pos, ok := pm.byAbs[entryBci+off]
	return pos, ok
}

// LineToAbs returns the lowest absolute_bci whose position is (file, line), for
// translating a gutter-click breakpoint to a bytecode location.
func (pm PositionMap) LineToAbs(file string, line int) (int, bool) {
	abs, ok := pm.lowestByLine[file+":"+strconv.Itoa(line)]
	return abs, ok
}

// MethodForAbs finds the method containing absolute bci abs: the one with the
// largest EntryBci <= abs. Returns its id and off = abs - EntryBci. Mirrors the
// VM's method-from-absolute-bci.
func MethodForAbs(reg map[int]Method, abs int) (id, off int, ok bool) {
	bestID, bestEntry := 0, -1
	for mid, m := range reg {
		if m.EntryBci <= abs && m.EntryBci > bestEntry {
			bestID, bestEntry = mid, m.EntryBci
		}
	}
	if bestEntry < 0 {
		return 0, 0, false
	}
	return bestID, abs - bestEntry, true
}
