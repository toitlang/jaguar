// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func SimulateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "simulate",
		Short:        "Start a simulated jaguar device on your machine",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			port, err := cmd.Flags().GetUint("port")
			if err != nil {
				return err
			}

			toitvm, err := Toitvm()
			if err != nil {
				return err
			}

			jaguarEntryPoint, ok := os.LookupEnv(EntryPointEnv)
			if !ok {
				return fmt.Errorf("You must set the env variable '%s'", EntryPointEnv)
			}

			simCmd := toitvm.Cmd(ctx, "-b", "none", jaguarEntryPoint, strconv.Itoa(int(port)))
			simCmd.Stderr = os.Stderr
			simCmd.Stdout = os.Stdout
			return simCmd.Run()
		},
	}

	cmd.Flags().UintP("port", "p", 0, "Port to run the simulator on")

	return cmd
}
