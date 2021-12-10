// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"io/ioutil"
	"os"
	"path/filepath"

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

			snapshotsCache, err := GetSnapshotsCachePath()
			if err != nil {
				return err
			}
			snapshot := filepath.Join(snapshotsCache, device.Name+".snapshot")

			entrypoint := args[0]
			buildSnap := sdk.Toitc(ctx, "-w", snapshot, entrypoint)
			buildSnap.Stderr = os.Stderr
			buildSnap.Stdout = os.Stdout
			if err := buildSnap.Run(); err != nil {
				return err
			}

			image, err := os.CreateTemp("", "*.image")
			if err != nil {
				return err
			}
			image.Close()
			defer os.Remove(image.Name())

			bits := "-m32"
			if device.WordSize == 8 {
				bits = "-m64"
			}

			buildImage := sdk.Toitvm(ctx, sdk.SnapshotToImagePath(), "--binary", bits, snapshot, image.Name())
			buildImage.Stderr = os.Stderr
			buildImage.Stdout = os.Stdout
			if err := buildImage.Run(); err != nil {
				return err
			}

			b, err := ioutil.ReadFile(image.Name())
			if err != nil {
				return err
			}
			return device.Run(ctx, b)
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "how long to scan")
	return cmd
}
