// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
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
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			entrypoint := args[0]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't run directory: '%s'", entrypoint)
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

			fmt.Printf("Running '%s' on '%s' ...\n", entrypoint, device.Name)
			b, err := sdk.Build(ctx, device, entrypoint)
			if err != nil {
				// We assume the error has been printed.
				// Mark the command as silent to avoid printing the error twice.
				cmd.SilenceErrors = true
				return err
			}
			if err := device.Run(ctx, sdk, b); err != nil {
				fmt.Println("Error:", err)
				// We just printed the error.
				// Mark the command as silent to avoid printing the error twice.
				cmd.SilenceErrors = true
				return err
			}
			fmt.Printf("Success: Sent %dKB code to '%s'\n", len(b)/1024, device.Name)
			return nil
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	return cmd
}
