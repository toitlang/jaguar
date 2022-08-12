// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"io"
	"os"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
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

			id := uuid.New()
			var name string
			if cmd.Flags().Changed("name") {
				name, err = cmd.Flags().GetString("name")
				if err != nil {
					return err
				}
			} else {
				name = GetRandomName(id[:])
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			snapshot, err := directory.GetJaguarSnapshotPath()
			if err != nil {
				return err
			}

			outReader, outWriter := io.Pipe()

			// Goroutine that gets data from the pipe and converts it into
			// lines.
			go func() {
				scanner := bufio.NewScanner(outReader)

				decoder := Decoder{scanner, cmd}

				decoder.decode()
			}()

			simCmd := sdk.ToitRun(ctx, snapshot, strconv.Itoa(int(port)), id.String(), name)
			simCmd.Stderr = os.Stderr
			simCmd.Stdout = outWriter
			return simCmd.Run()
		},
	}

	cmd.Flags().UintP("port", "p", 0, "port to run the simulator on")
	cmd.Flags().String("name", "", "name for the simulator, if not set a name will be auto generated")

	return cmd
}
