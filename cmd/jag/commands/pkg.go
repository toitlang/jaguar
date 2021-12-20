// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"

	"github.com/spf13/cobra"
)

func PkgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "pkg",
		Short:        "Manage your Toit packages",
		Long:         "Manage your Toit packages.\n\nUse 'jag pkg -- --help' for detailed help.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			toitPkg := sdk.ToitPkg(ctx, append([]string{"pkg"}, args...))
			toitPkg.Stdin = os.Stdin
			toitPkg.Stdout = os.Stdout
			toitPkg.Stderr = os.Stderr
			if err := toitPkg.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}
