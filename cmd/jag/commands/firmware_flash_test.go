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

func TestDeviceOptionsJaguarConfigUartOnly(t *testing.T) {
	withoutFlag := (DeviceOptions{}).getJaguarConfig()
	if _, ok := withoutFlag["jag.uart-only"]; ok {
		t.Fatal("jag.uart-only is present without --uart-only")
	}

	withFlag := (DeviceOptions{UartOnly: true}).getJaguarConfig()
	if value, ok := withFlag["jag.uart-only"]; !ok || value != true {
		t.Fatalf("jag.uart-only is %#v, want true", value)
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
		{
			name: "uart-only default",
			args: []string{"--uart-only"},
			want: map[string]interface{}{"baud": uint(defaultProxyBaudRate)},
		},
		{
			name: "uart-only explicit",
			args: []string{"--uart-only", "--uart-endpoint-baud=115200"},
			want: map[string]interface{}{"baud": uint(115200)},
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

func TestUartOnlyRejectsDisabledEndpoint(t *testing.T) {
	cmd := &cobra.Command{}
	addFirmwareFlashFlags(cmd, "device name")
	if err := cmd.ParseFlags([]string{"--uart-only", "--uart-endpoint-baud=0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := getUartEndpointOptions(cmd); err == nil {
		t.Fatal("--uart-only accepted an explicitly disabled UART endpoint")
	}
}

func TestFirmwareCommandsHavePublicUartFlags(t *testing.T) {
	commands := []*cobra.Command{FlashCmd(), FirmwareUpdateCmd(), FirmwareExtractCmd()}
	for _, command := range commands {
		for _, name := range []string{"uart-endpoint-baud", "uart-only"} {
			flag := command.Flags().Lookup(name)
			if flag == nil {
				t.Fatalf("%s is missing --%s flag", command.CommandPath(), name)
			}
			if flag.Hidden {
				t.Fatalf("%s hides --%s flag", command.CommandPath(), name)
			}
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
