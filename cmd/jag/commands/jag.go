// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import "github.com/spf13/cobra"

type Info struct {
	Version    string
	Date       string
	SDKVersion string
}

func JagCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jag",
		Short: "Fast development for your ESP32",
		Long: "Jaguar is a Toit application for your ESP32 that gives you the fastest development cycle.\n\n" +
			"Jaguar uses the capabilities of the Toit virtual machine to let you update and restart your\n" +
			"ESP32 applications written in Toit over WiFi. Change your Toit code in your editor, update\n" +
			"the application on your device, and restart it all within seconds. No need to flash over\n" +
			"serial, reboot your device, or wait for it to reconnect to your network.",
	}

	cmd.AddCommand(
		ScanCmd(),
		PingCmd(),
		RunCmd(),
		SimulateCmd(),
		DecodeCmd(),
		SetupCmd(info),
		FlashCmd(),
		MonitorCmd(),
		SetPortCmd(),
		VersionCmd(info),
	)
	return cmd
}
