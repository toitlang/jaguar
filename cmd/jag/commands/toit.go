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
		Short: "Run 'toit' commands through Jaguar",
		Long: "Run the downloaded 'toit' command with the given arguments.\n\n" +
			"Calling 'jag toit --help' provides the help of 'toit'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			// Skip "lsp" which is handled by a dedicated command.
			if args[0] == "lsp" {
				return nil
			}

			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			passThrough := sdk.PassThrough(ctx, args)
			passThrough.Stdin = os.Stdin
			passThrough.Stdout = os.Stdout
			passThrough.Stderr = os.Stderr
			return passThrough.Run()
		},
		// Don't print usage on error since we're passing through.
		SilenceUsage: true,
		// Disable Cobra's flag parsing entirely to pass all flags through
		DisableFlagParsing: true,
		// Disable the auto-generated tag in documentation output.
		DisableAutoGenTag: true,
	}

	// Add custom help function that passes through to the underlying command
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		// Check if we have a valid context
		ctx := cmd.Context()
		if ctx == nil {
			// Fall back to showing our own help if context is nil
			// This happens when using `jag help toit` instead of `jag toit --help`
			cmd.Println(cmd.Long)
			return
		}

		// Get the SDK only if we have a valid context
		sdk, err := GetSDK(ctx)
		if err != nil {
			cmd.PrintErr(err)
			return
		}

		// Combine the command args with the help flag
		args = append(args, "-h")
		passThrough := sdk.PassThrough(ctx, args)
		passThrough.Stdin = os.Stdin
		passThrough.Stdout = os.Stdout
		passThrough.Stderr = os.Stderr
		if err := passThrough.Run(); err != nil {
			cmd.PrintErr(err)
		}
	})

	cmd.AddCommand(
		ToitLspCmd(),
	)
	return cmd
}

func ToitLspCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "lsp",
		Short:        "Start the Toit LSP server",
		SilenceUsage: true,
		Hidden:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			cmd.SilenceErrors = true
			toitLsp := sdk.ToitLsp(ctx, args)
			toitLsp.Stdin = os.Stdin
			toitLsp.Stdout = os.Stdout
			toitLsp.Stderr = os.Stderr
			return toitLsp.Run()
		},
	}
	return cmd
}
