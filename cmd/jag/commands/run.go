// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func RunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <file>",
		Short: "Run Toit code on a Jaguar device",
		Long: "Run the specified .toit file on a Jaguar device as a new program. If the\n" +
			"device is already executing another program, that program is stopped before\n" +
			"the new program is started.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			device, err := GetDevice(ctx, cfg, true)
			if err != nil {
				return err
			}

			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			entrypoint := args[0]
			fmt.Printf("Running '%s' on device '%s'...\n", entrypoint, device.Name)
			b, err := sdk.Build(ctx, device, entrypoint)
			if err != nil {
				return err
			}
			if err := device.Run(ctx, b); err != nil {
				return nil
			}
			fmt.Println("Successfully pushed program to device.")
			return nil
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "how long to scan")
	return cmd
}
