// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
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

			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			var snapshot string
			if system {
				// TODO(kasper): This is now wrong. We shouldn't try to use the Jaguar snapshot
				// to decode system errors.
				var err error
				if snapshot, err = directory.GetJaguarSnapshotPath(); err != nil {
					return err
				}
			} else {
				deviceSelect, err := parseDeviceFlag(cmd)
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
				snapshot = filepath.Join(snapshotsCache, device.ID+".snapshot")
			}

			decodeCmd := sdk.ToitRun(ctx, sdk.SystemMessageSnapshotPath(), snapshot, "-b", args[0])
			decodeCmd.Stderr = os.Stderr
			decodeCmd.Stdout = os.Stdout
			return decodeCmd.Run()
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	cmd.Flags().Bool("system", false, "if set, will decode the system message using the Jaguar snapshot")
	return cmd
}
