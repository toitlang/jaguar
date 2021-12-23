// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func PingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ping",
		Short:        "Ping a Jaguar device to see if it is active",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			device, err := GetDevice(ctx, cfg, false, deviceSelect)
			if err != nil {
				return err
			}
			if !device.Ping(ctx) {
				cmd.SilenceUsage = true
				return fmt.Errorf("couldn't ping the device")
			}

			fmt.Println("Got ping from the device")
			return nil
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	cmd.Flags().DurationP("timeout", "t", pingTimeout, "how long to wait for a reply")
	return cmd
}
