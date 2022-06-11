// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func LogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Live stream logs from a Jaguar device",
		Long: "Connects over network to the devices and retrieves live logs from the system.\n" +
			".",
		Args:         cobra.ExactArgs(0),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			snapshotsCache, err := directory.GetSnapshotsCachePath()
			if err != nil {
				return err
			}

			fmt.Printf("Streaming logs from  '%s' ...\n", device.Name)

			if err := device.Log(ctx, sdk, snapshotsCache); err != nil {
				fmt.Println("Error:", err)
				// We just printed the error.
				// Mark the command as silent to avoid printing the error twice.
				cmd.SilenceErrors = true
				return err
			}
			fmt.Printf("Disconnected from '%s'\n", device.Name)
			return nil
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	return cmd
}
