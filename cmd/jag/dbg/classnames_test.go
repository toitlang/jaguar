// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

import "testing"

func TestParseClassNamesAndResolve(t *testing.T) {
	dump := "0 Object\n16 String_\n47 Point\n\ngarbage line\nx y\n"
	cn := ParseClassNames(dump)
	if len(cn) != 3 {
		t.Fatalf("got %d classes, want 3: %v", len(cn), cn)
	}
	cases := []struct{ in, want string }{
		{"<obj:47>", "<obj:Point>"},   // known instance
		{"<obj:16>", "<obj:String_>"}, // known internal class
		{"<obj:99>", "<obj:99>"},      // unknown id: unchanged
		{"42", "42"},                  // smi: unchanged
		{"3.14", "3.14"},              // double: unchanged
		{"null", "null"},              // scalar: unchanged
		{"<obj>", "<obj>"},            // legacy marker (no id): unchanged
		{"<obj:abc>", "<obj:abc>"},    // malformed id: unchanged
	}
	for _, c := range cases {
		if got := cn.Resolve(c.in); got != c.want {
			t.Errorf("Resolve(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveNilClassNames(t *testing.T) {
	var cn ClassNames // nil map: must not panic, passes values through
	if got := cn.Resolve("<obj:47>"); got != "<obj:47>" {
		t.Errorf("nil Resolve = %q, want unchanged", got)
	}
}
