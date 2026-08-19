// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"regexp"
	"strconv"
	"strings"
)

// NameMap maps method names to/from their entry bci (the absolute bytecode
// position of a method's first instruction), built offline from a snapshot.
type NameMap struct {
	NameToEntry map[string]int
	EntryToName map[int]string
	// EntrySDK[entry_bci] is true when the method is defined in the Toit SDK
	// (its source path is reported as "<sdk>/..."), as opposed to the user's
	// program or its packages. Used to filter the noisy `m` listing.
	EntrySDK map[int]bool
}

var (
	// Header line: "<dispatch_idx>: <name> <file>:<line>:<col>". Header lines
	// start at column 0; bytecode lines are indented, so this is unambiguous.
	headerRe = regexp.MustCompile(`^\d+: (.+)$`)
	// Source-location trailing token: "<path>:<line>:<col>".
	locRe = regexp.MustCompile(`.+:\d+:\d+$`)
	// First bytecode of a method: "  0/ <entry_bci> [..]".
	firstByteRe = regexp.MustCompile(`^\s+0/\s*(\d+)\s+\[`)
)

// ParseBytecodes builds a NameMap from `toit tool snapshot bytecodes <snap>`
// output. Pure: callers shell out and pass the captured stdout. Port of the
// Python driver's build_name_map.
func ParseBytecodes(output string) NameMap {
	nm := NameMap{NameToEntry: map[string]int{}, EntryToName: map[int]string{}, EntrySDK: map[int]bool{}}
	current := ""
	currentSDK := false
	have := false
	for _, line := range strings.Split(output, "\n") {
		if m := headerRe.FindStringSubmatch(line); m != nil {
			rest := m[1]
			fields := strings.Fields(rest)
			if len(fields) >= 1 && locRe.MatchString(fields[len(fields)-1]) {
				loc := fields[len(fields)-1]
				// Strip only the last whitespace token (the source location);
				// names themselves may contain spaces.
				name := strings.TrimSpace(strings.TrimSuffix(rest, loc))
				current = name
				currentSDK = strings.HasPrefix(loc, "<sdk>")
				have = true
				continue
			}
		}
		if have {
			if bm := firstByteRe.FindStringSubmatch(line); bm != nil {
				entry, _ := strconv.Atoi(bm[1])
				nm.NameToEntry[current] = entry
				nm.EntryToName[entry] = current
				nm.EntrySDK[entry] = currentSDK
				have = false
			}
		}
	}
	return nm
}

// Resolver maps method names to/from the VM's numeric method ids, obtained by
// cross-referencing the offline NameMap (name<->entry_bci) with the runtime
// method registry (id->entry_bci) on entry_bci.
type Resolver struct {
	NameToID map[string]int
	IDToName map[int]string
}

// NewResolver cross-references a NameMap with the dbg:methods registry.
func NewResolver(names NameMap, methods map[int]Method) *Resolver {
	r := &Resolver{NameToID: map[string]int{}, IDToName: map[int]string{}}
	for id, m := range methods {
		if name, ok := names.EntryToName[m.EntryBci]; ok {
			r.NameToID[name] = id
			r.IDToName[id] = name
		}
	}
	return r
}
