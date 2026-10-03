// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// debugCapableSDK returns an SDK whose inner toit.run accepts --debug, or skips.
func debugCapableSDK(t *testing.T) *SDK {
	t.Helper()
	ctx := SetInfo(context.Background(), Info{})
	sdk, err := GetSDK(ctx)
	if err != nil {
		t.Skipf("no SDK discoverable (set JAG_TOIT_REPO_PATH): %v", err)
	}
	// Probe: the inner runner must exist and accept --debug.
	if _, err := os.Stat(sdk.InnerToitRunPath()); err != nil {
		t.Skipf("inner toit.run not found at %s: %v", sdk.InnerToitRunPath(), err)
	}
	out, _ := exec.Command(sdk.InnerToitRunPath(), "--help").CombinedOutput()
	_ = out // --help may not advertise --debug; fall through and let the run prove it.
	return sdk
}

func TestDebugEndToEnd(t *testing.T) {
	debugCapableSDK(t)

	// Write a script that breaks in count-to, continues into it, inspects, and resumes.
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "cmds.txt")
	script := "b count-to\nc\ni\nc\nc\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}

	target, err := filepath.Abs(filepath.Join("testdata", "count_to.toit"))
	if err != nil {
		t.Fatal(err)
	}

	// Capture os.Stdout for the duration of the command.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	cmd := DebugCmd()
	cmd.SetContext(SetInfo(context.Background(), Info{}))
	cmd.SetArgs([]string{"-d", "host", "--script", scriptPath, target})
	runErr := cmd.Execute()

	w.Close()
	os.Stdout = old
	transcript := <-done

	if runErr != nil {
		t.Fatalf("jag debug failed: %v\ntranscript:\n%s", runErr, transcript)
	}
	if !strings.Contains(transcript, "paused in count-to") {
		t.Errorf("expected 'paused in count-to' (proves name resolution); transcript:\n%s", transcript)
	}
	if !strings.Contains(transcript, "result=10") {
		t.Errorf("expected program output 'result=10'; transcript:\n%s", transcript)
	}
}
