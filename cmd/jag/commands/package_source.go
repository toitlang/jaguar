// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

// pkgRoots maps a package id — the key in package.lock's `packages:` map, which
// the compiler emits as "<pkg:ID>" in position paths — to its on-disk root
// directory. Used to resolve package source so the debugger can show it (e.g.
// when stepping In to a package method).
type pkgRoots map[string]string

// pkgFilePath maps a compiler "<pkg:ID>/relative" position token to its on-disk
// candidate path: <root>/src/<relative>, where root comes from roots[ID]. Toit
// packages keep their source under src/. Pure: it does not touch the filesystem.
// Returns ("", false) for a malformed token or an unknown id.
func pkgFilePath(file string, roots pkgRoots) (string, bool) {
	if !strings.HasPrefix(file, "<pkg:") {
		return "", false
	}
	rest := strings.TrimPrefix(file, "<pkg:")
	i := strings.Index(rest, ">/")
	if i < 0 {
		return "", false
	}
	id, relative := rest[:i], rest[i+2:]
	root, ok := roots[id]
	if !ok {
		return "", false
	}
	return filepath.Join(root, "src", relative), true
}

// loadPkgRoots parses <dir>/package.lock and returns the id->root map. A path
// package resolves to <dir>/<path>; a registry package to
// <dir>/.packages/<url>/<version> (where its source was downloaded). A
// missing/invalid lock yields an empty map (package source simply won't
// resolve, and the UI falls back to the "no source" banner).
func loadPkgRoots(dir string) pkgRoots {
	data, err := os.ReadFile(filepath.Join(dir, "package.lock"))
	if err != nil {
		return pkgRoots{}
	}
	var lf struct {
		Packages map[string]struct {
			Path    string `yaml:"path"`
			URL     string `yaml:"url"`
			Version string `yaml:"version"`
		} `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return pkgRoots{}
	}
	roots := pkgRoots{}
	for id, p := range lf.Packages {
		switch {
		case p.Path != "":
			roots[id] = filepath.Join(dir, p.Path)
		case p.URL != "" && p.Version != "":
			roots[id] = filepath.Join(dir, ".packages", p.URL, p.Version)
		}
	}
	return roots
}
