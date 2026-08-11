// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestFirmwareCommandsHaveDisableUDPFlag(t *testing.T) {
	tests := []struct {
		name    string
		command *cobra.Command
	}{
		{"flash", FlashCmd()},
		{"firmware update", FirmwareUpdateCmd()},
		{"firmware extract", FirmwareExtractCmd()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := test.command.Flags().Lookup("disable-udp")
			if flag == nil {
				t.Fatal("missing --disable-udp flag")
			}
			if flag.DefValue != "false" {
				t.Fatalf("--disable-udp default is %q, want false", flag.DefValue)
			}
		})
	}
}

func TestDeviceOptionsJaguarConfigDisableUDP(t *testing.T) {
	withoutFlag := (DeviceOptions{}).getJaguarConfig()
	if _, ok := withoutFlag["jag.disable-udp"]; ok {
		t.Fatal("jag.disable-udp is present without --disable-udp")
	}

	withFlag := (DeviceOptions{DisableUDP: true}).getJaguarConfig()
	if value, ok := withFlag["jag.disable-udp"]; !ok || value != true {
		t.Fatalf("jag.disable-udp is %#v, want true", value)
	}
}
