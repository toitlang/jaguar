// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"strings"
	"testing"
)

func TestDebugCmdRejectsNonHostDevice(t *testing.T) {
	cmd := DebugCmd()
	cmd.SetArgs([]string{"-d", "esp32", "foo.toit"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "device debugging is not yet supported (only -d host)") {
		t.Fatalf("expected non-host rejection, got %v", err)
	}
}

func TestDebugCmdHasScriptFlag(t *testing.T) {
	cmd := DebugCmd()
	if cmd.Flags().Lookup("script") == nil {
		t.Errorf("expected --script flag")
	}
	if cmd.Flags().Lookup("device") == nil {
		t.Errorf("expected --device/-d flag")
	}
}

func TestDebugCmdHasWebFlag(t *testing.T) {
	cmd := DebugCmd()
	if cmd.Flags().Lookup("web") == nil {
		t.Errorf("expected --web flag")
	}
}

func TestDebugCmdRejectsWebAndScript(t *testing.T) {
	cmd := DebugCmd()
	cmd.SetArgs([]string{"-d", "host", "--web", "--script", "x.txt", "foo.toit"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--web and --script are mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}
