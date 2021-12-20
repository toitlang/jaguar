// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"

	"github.com/spf13/cobra"
)

func ToitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "toit",
		Short: "Use 'toit lsp' and other extras through Jaguar",
	}

	cmd.AddCommand(
		ToitLspCmd(),
	)
	return cmd
}

func ToitLspCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "lsp",
		Short:        "Start the Toit LSP server",
		Long:         "Start the Toit LSP server.\n\nUse 'jag toit lsp -- --help' for detailed help.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			toitLsp := sdk.ToitLsp(ctx, append([]string{"--toitc", sdk.ToitcPath()}, args...))
			toitLsp.Stdin = os.Stdin
			toitLsp.Stdout = os.Stdout
			toitLsp.Stderr = os.Stderr
			if err := toitLsp.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func ToitPkgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "pkg",
		Short:        "Run the Toit package manager",
		Long:         "Run the Toit package manager.\n\nUse 'jag toit pkg -- --help' for detailed help.",
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
