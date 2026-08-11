// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestExplicitUnavailablePortFails(t *testing.T) {
	const unavailablePort = "/jaguar-test-port-that-does-not-exist"

	commands := []struct {
		name string
		cmd  func() *cobra.Command
	}{
		{"flash", FlashCmd},
		{"monitor", MonitorCmd},
	}

	for _, test := range commands {
		t.Run(test.name, func(t *testing.T) {
			cmd := test.cmd()
			cmd.SetArgs([]string{"--port", unavailablePort})
			cmd.SilenceErrors = true

			err := cmd.Execute()
			if err == nil {
				t.Fatal("command succeeded with an unavailable explicit port")
			}
			if !strings.Contains(err.Error(), unavailablePort) {
				t.Fatalf("error %q does not identify the unavailable port", err)
			}
		})
	}
}
