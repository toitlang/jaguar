// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func SimulateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Start a simulated Jaguar device on your machine",
		Long: "Start a simulated Jaguar device on your host machine. Useful for testing\n" +
			"and for experimenting with the Jaguar-based workflows.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			port, err := cmd.Flags().GetUint("port")
			if err != nil {
				return err
			}

			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			snapshot, err := GetSnapshotPath()
			if err != nil {
				return err
			}

			simCmd := sdk.Toitvm(ctx, "-b", "none", snapshot, strconv.Itoa(int(port)))
			simCmd.Stderr = os.Stderr
			simCmd.Stdout = os.Stdout
			return simCmd.Run()
		},
	}

	cmd.Flags().UintP("port", "p", 0, "port to run the simulator on")

	return cmd
}
