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
		Hidden:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			cmd.SilenceErrors = true
			toitLsp := sdk.ToitLsp(ctx, append([]string{"--toitc", sdk.ToitCompilePath()}, args...))
			toitLsp.Stdin = os.Stdin
			toitLsp.Stdout = os.Stdout
			toitLsp.Stderr = os.Stderr
			return toitLsp.Run()
		},
	}
	return cmd
}
