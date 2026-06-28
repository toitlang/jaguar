// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

func TestWebDriverEndToEnd(t *testing.T) {
	sdk := debugCapableSDK(t)
	ctx := SetInfo(context.Background(), Info{})

	target, err := filepath.Abs(filepath.Join("testdata", "count_to.toit"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "prog.snapshot")
	if err := sdk.Compile(ctx, snapshot, target, -1); err != nil {
		t.Fatalf("compile: %v", err)
	}

	posOut, err := sdk.SnapshotPositions(ctx, snapshot)
	if err != nil {
		t.Skipf("SDK lacks 'snapshot positions' (rebuild debug SDK): %v", err)
	}
	pm := dbg.ParsePositions(string(posOut))

	bytecodes, err := sdk.SnapshotBytecodes(ctx, snapshot)
	if err != nil {
		t.Fatalf("bytecodes: %v", err)
	}
	names := dbg.ParseBytecodes(string(bytecodes))

	channel, err := newStdioChannel(sdk.ToitRunDebug(ctx, snapshot))
	if err != nil {
		t.Fatalf("launch VM: %v", err)
	}
	defer channel.Close()
	session := dbg.NewSession(channel, names, os.Stdout)
	if err := session.Start(); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if err := session.Methods(); err != nil {
		t.Fatalf("methods: %v", err)
	}

	driver := newWebDriver(session, pm, filepath.Dir(target), filepath.Join(sdk.Path, "lib"))

	// The positions dump emits the absolute source path for user files — exactly
	// the `target` path we compiled. Line 8 is the count-to for-header (captured
	// in Task 1: bcis 292/304). Guard in case the dump shape differs on a rebuild.
	if _, ok := pm.LineToAbs(target, 8); !ok {
		t.Skipf("no bytecode mapped to %s:8; positions dump shape differs", target)
	}

	if _, err := driver.handleCmd(command{Verb: "break", File: target, Line: 8}); err != nil {
		t.Fatalf("set breakpoint on %s:8: %v", target, err)
	}
	st, err := driver.handleCmd(command{Verb: "continue"})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if st.Status != "paused" || st.Location == nil || st.Location.Line != 8 {
		t.Fatalf("after continue, want paused at line 8, got %+v (loc=%+v)", st, st.Location)
	}

	// Step at least once and confirm we are still paused with a location.
	st, err = driver.handleCmd(command{Verb: "step"})
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if st.Status != "paused" {
		t.Errorf("after step, want paused, got %q (%+v)", st.Status, st)
	} else if st.Location == nil {
		t.Errorf("paused step should carry a location, got %+v", st)
	}
}
