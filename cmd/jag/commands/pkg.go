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
		Use:   "pkg",
		Short: "Run 'toit pkg' commands through Jaguar",
		Long: "Run 'toit pkg' of the downloaded 'toit' executable with the given arguments.\n\n" +
			"Calling 'jag pkg --help' provides the help of 'toit pkg'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			pkgArgs := append([]string{"pkg"}, args...)
			passThrough := sdk.PassThrough(ctx, pkgArgs)
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
			// This happens when using `jag help pkg` instead of `jag pkg --help`
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
		pkgArgs := append([]string{"pkg"}, append(args, "-h")...)
		passThrough := sdk.PassThrough(ctx, pkgArgs)
		passThrough.Stdin = os.Stdin
		passThrough.Stdout = os.Stdout
		passThrough.Stderr = os.Stderr
		if err := passThrough.Run(); err != nil {
			cmd.PrintErr(err)
		}
	})

	return cmd
}
