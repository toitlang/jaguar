// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import (
	"strconv"
	"strings"
)

// ClassNames maps a numeric class id to its class name, built offline from
// `toit tool snapshot class-names`. The debugger emits heap-object registers as
// "<obj:<class_id>>" (numeric); the operator side resolves the id to a name with
// this map, keeping the wire protocol "VM numeric, names resolved offline".
type ClassNames map[int]string

// ParseClassNames parses the class-names dump: one line per class, "<id> <name>".
// Class names contain no spaces; lines that do not parse are skipped.
func ParseClassNames(dump string) ClassNames {
	cn := ClassNames{}
	for _, line := range strings.Split(dump, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cn[id] = strings.Join(fields[1:], " ")
	}
	return cn
}

// Resolve rewrites a raw register value: an "<obj:N>" whose N is a known class id
// becomes "<obj:Name>". Scalars ("42", "3.14", "null", "true"), an unknown id, or
// a malformed marker are returned unchanged.
func (cn ClassNames) Resolve(value string) string {
	if cn == nil || !strings.HasPrefix(value, "<obj:") || !strings.HasSuffix(value, ">") {
		return value
	}
	idStr := value[len("<obj:") : len(value)-1]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return value
	}
	name, ok := cn[id]
	if !ok {
		return value
	}
	return "<obj:" + name + ">"
}
