// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func PingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ping",
		Short:        "Ping a Jaguar device to see if it is active",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, sdk, false, deviceSelect)
			if err != nil {
				return err
			}
			if !device.Ping(ctx, sdk) {
				cmd.SilenceUsage = true
				return fmt.Errorf("couldn't ping the device")
			}

			fmt.Println("Got pong from the device")
			return nil
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	cmd.Flags().DurationP("timeout", "t", pingTimeout, "how long to wait for a reply")
	return cmd
}
