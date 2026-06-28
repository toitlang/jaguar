// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"path/filepath"
	"testing"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func TestInnerToitRunPath(t *testing.T) {
	s := &SDK{Path: filepath.Join("some", "sdk")}
	want := filepath.Join("some", "sdk", "lib", "toit", "bin", directory.Executable("toit.run"))
	if got := s.InnerToitRunPath(); got != want {
		t.Errorf("InnerToitRunPath = %q, want %q", got, want)
	}
}
