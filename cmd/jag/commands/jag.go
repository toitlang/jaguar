// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/analytics"
	segment "gopkg.in/segmentio/analytics-go.v3"
)

type Info struct {
	Version    string
	Date       string
	SDKVersion string
}

func JagCmd(info Info) *cobra.Command {
	analyticsClient, err := analytics.GetClient()
	if err != nil {
		panic(err)
	}

	cmd := &cobra.Command{
		Use:   "jag",
		Short: "Fast development for your ESP32",
		Long: "Jaguar is a Toit application for your ESP32 that gives you the fastest development cycle.\n\n" +
			"Jaguar uses the capabilities of the Toit virtual machine to let you update and restart your\n" +
			"ESP32 applications written in Toit over WiFi. Change your Toit code in your editor, update\n" +
			"the application on your device, and restart it all within seconds. No need to flash over\n" +
			"serial, reboot your device, or wait for it to reconnect to your network.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			go analyticsClient.Enqueue(segment.Page{
				Name: "CLI Execute",
				Properties: segment.Properties{
					"command": (*cobra.Command)(cmd).UseLine(),
					"jaguar":  true,
				},
			})
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			analyticsClient.Close()
		},
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
		WatchCmd(),
		SetPortCmd(),
		ToitCmd(),
		PkgCmd(info, analyticsClient),
		ConfigCmd(),
		VersionCmd(info),
	)
	return cmd
}
