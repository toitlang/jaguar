// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"reflect"
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

func TestUartEndpointOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]interface{}
	}{
		{name: "disabled"},
		{
			name: "console",
			args: []string{"--uart-endpoint-baud=921600"},
			want: map[string]interface{}{"baud": uint(921600)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			addFirmwareFlashFlags(cmd, "device name")
			if err := cmd.ParseFlags(test.args); err != nil {
				t.Fatal(err)
			}

			got, err := getUartEndpointOptions(cmd)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("getUartEndpointOptions = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestFirmwareCommandsHavePublicUartEndpointBaudFlag(t *testing.T) {
	commands := []*cobra.Command{FlashCmd(), FirmwareUpdateCmd(), FirmwareExtractCmd()}
	for _, command := range commands {
		flag := command.Flags().Lookup("uart-endpoint-baud")
		if flag == nil {
			t.Fatalf("%s is missing --uart-endpoint-baud flag", command.CommandPath())
		}
		if flag.Hidden {
			t.Fatalf("%s hides --uart-endpoint-baud flag", command.CommandPath())
		}
	}
}

func TestFirmwareCommandsDoNotHaveUartEndpointRxFlag(t *testing.T) {
	commands := []*cobra.Command{FlashCmd(), FirmwareUpdateCmd(), FirmwareExtractCmd()}
	for _, command := range commands {
		if command.Flags().Lookup("uart-endpoint-rx") != nil {
			t.Fatalf("%s has --uart-endpoint-rx flag", command.CommandPath())
		}
	}
}
