// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func DecodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decode <message>",
		Short: "Decode a stack trace received from a Jaguar device",
		Long: "Decode a stack trace received from a Jaguar device. Stack traces are encoded\n" +
			"using base64 and are easy to copy from the serial output.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			system, err := cmd.Flags().GetBool("system")
			if err != nil {
				return err
			}

			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			var snapshot string
			if system {
				var ok bool
				snapshot, ok = os.LookupEnv(EntryPointEnv)
				if !ok {
					return fmt.Errorf("you must set the env variable '%s'", EntryPointEnv)
				}
			} else {
				device, err := GetDevice(ctx, cfg, true)
				if err != nil {
					return err
				}

				snapshotCache, err := GetSnapshotCachePath()
				if err != nil {
					return err
				}
				snapshot = filepath.Join(snapshotCache, device.Name+".snapshot")
			}

			decodeCmd := sdk.Toitvm(ctx, sdk.SystemMessageSnapshotPath(), snapshot, "-b", args[0])
			decodeCmd.Stderr = os.Stderr
			decodeCmd.Stdout = os.Stdout
			return decodeCmd.Run()
		},
	}

	cmd.Flags().Bool("system", false, "if set, will decode the system message using the Jaguar snapshot")
	return cmd
}
