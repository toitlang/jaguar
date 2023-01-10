// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func CompileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compile <file>",
		Short: "Compile Toit code to a snapshot",
		Long: "Compile Toit code to a snapshot file.  The snapshot is an executable\n" +
			"can be run on a Jaguar device as a new program.  The snapshot also\n" +
			"contains debug information that can be used by host-side tools.",
		Args:         cobra.ExactArgs(1),
		Hidden:       true,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			optimizationLevel := -1
			if cmd.Flags().Changed("optimization-level") {
				optimizationLevel, err = cmd.Flags().GetInt("optimization-level")
				if err != nil {
					return err
				}
			}

			outputfile := ""
			if !cmd.Flags().Changed("output") {
				dot := strings.LastIndex(entrypoint, ".")
				slash := strings.LastIndex(entrypoint, "/")
				backslash := strings.LastIndex(entrypoint, "\\")
				fail := false
				if dot == -1 {
					fail = true
				} else if slash != -1 && slash > dot {
					fail = true
				} else if backslash != -1 && backslash > dot {
					fail = true
				}
				if fail {
					return fmt.Errorf("can't construct snapshot name from directory: '%s'", entrypoint)
				}
				outputfile = entrypoint[0:dot] + ".snapshot"
			} else {
				outputfile, err = cmd.Flags().GetString("output")
				if err != nil {
					return err
				}
			}

			fmt.Printf("Compiling '%s' to '%s'\n", entrypoint, outputfile)

			err = sdk.Compile(ctx, outputfile, entrypoint, optimizationLevel)
			if err != nil {
				// We assume the error has been printed.
				// Mark the command as silent to avoid printing the error twice.
				cmd.SilenceErrors = true
				return err
			}

			fmt.Printf("Success: Wrote compiled bytecodes to '%s'\n", outputfile)
			return nil
		},
	}

	cmd.Flags().StringP("output", "o", "", "specify output (snapshot) file")
	cmd.Flags().IntP("optimization-level", "O", -1, "optimization level")
	return cmd
}
