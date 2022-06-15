// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/analytics"
	segment "gopkg.in/segmentio/analytics-go.v3"
)

type ctxKey string

const (
	ctxKeyInfo ctxKey = "info"
)

type Info struct {
	Version    string `mapstructure:"version" yaml:"version" json:"version"`
	Date       string `mapstructure:"date" yaml:"date" json:"date"`
	SDKVersion string `mapstructure:"sdkVersion" yaml:"sdkVersion" json:"sdkVersion"`
}

func SetInfo(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, ctxKeyInfo, info)
}

func GetInfo(ctx context.Context) Info {
	return ctx.Value(ctxKeyInfo).(Info)
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
		CompileCmd(),
		SimulateCmd(),
		DecodeCmd(),
		SetupCmd(info),
		FlashCmd(),
		FirmwareCmd(),
		MonitorCmd(),
		WatchCmd(),
		PortCmd(),
		ToitCmd(),
		PkgCmd(info, analyticsClient),
		ConfigCmd(),
		VersionCmd(info),
	)
	return cmd
}
