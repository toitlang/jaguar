// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// pkgFilePath maps a compiler "<pkg:ID>/relative" position token to its on-disk
// candidate path: <packageRoot>/src/<relative>, where the id is the key in
// package.lock's `packages:` map.
func TestPkgFilePath(t *testing.T) {
	roots := pkgRoots{
		"..":       "/proj/fuzzy_logic",
		"pkg-host": "/proj/examples/.packages/github.com/toitlang/pkg-host/1.17.0",
	}
	cases := []struct {
		file string
		want string
		ok   bool
	}{
		{"<pkg:..>/json_loader.toit", "/proj/fuzzy_logic/src/json_loader.toit", true},
		{"<pkg:pkg-host>/os.toit", "/proj/examples/.packages/github.com/toitlang/pkg-host/1.17.0/src/os.toit", true},
		{"<pkg:unknown>/x.toit", "", false},  // id not in the lock
		{"<sdk>/core/print.toit", "", false}, // not a pkg token
		{"plain.toit", "", false},
	}
	for _, c := range cases {
		got, ok := pkgFilePath(c.file, roots)
		if ok != c.ok || (ok && got != filepath.FromSlash(c.want)) {
			t.Errorf("pkgFilePath(%q) = %q,%v; want %q,%v", c.file, got, ok, c.want, c.ok)
		}
	}
}

// loadPkgRoots parses package.lock, mapping each package id to its root: a path
// package to <dir>/<path>, a registry package to <dir>/.packages/<url>/<version>.
func TestLoadPkgRoots(t *testing.T) {
	dir := t.TempDir()
	lock := `sdk: ^2.0.0
prefixes:
  fuzzy-logic: ..
packages:
  ..:
    path: ..
  pkg-host:
    url: github.com/toitlang/pkg-host
    name: host
    version: 1.17.0
    hash: 05a26b5fa1cc73dc64a5e9cc16c40ae5aa24e0fb
`
	if err := os.WriteFile(filepath.Join(dir, "package.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := loadPkgRoots(dir)
	if got, want := roots[".."], filepath.Join(dir, ".."); got != want {
		t.Errorf("roots[..] = %q, want %q", got, want)
	}
	if got, want := roots["pkg-host"], filepath.Join(dir, ".packages", "github.com/toitlang/pkg-host", "1.17.0"); got != want {
		t.Errorf("roots[pkg-host] = %q, want %q", got, want)
	}
}

// A missing lock yields an empty map, not an error (source just won't resolve).
func TestLoadPkgRootsMissingLock(t *testing.T) {
	if roots := loadPkgRoots(t.TempDir()); len(roots) != 0 {
		t.Errorf("missing lock: want empty roots, got %v", roots)
	}
}
