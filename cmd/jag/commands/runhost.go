// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"

	"github.com/spf13/cobra"
)

func RunHostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runhost <file> <arguments>",
		Short: "Run Toit code on your own workstation",
		Long: "Run the specified .toit or .snapshot file on the current machine.\n" +
			"If you use features only available on embedded platforms you will get" +
			"an 'unimplemented primitive' exception.",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

            runCmd := sdk.ToitRun(ctx, args...)
            runCmd.Stderr = os.Stderr
            runCmd.Stdout = os.Stdout
            runCmd.Stdin = os.Stdin
            return runCmd.Run()
		},
	}

	return cmd
}
