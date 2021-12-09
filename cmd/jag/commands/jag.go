// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import "github.com/spf13/cobra"

func JagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jag",
		Short: "Jaguar is a very fast car for ESP32",
	}

	cmd.AddCommand(
		ScanCmd(),
		PingCmd(),
		RunCmd(),
		SimulateCmd(),
		DecodeCmd(),
	)
	return cmd
}
