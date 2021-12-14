// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func VersionCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Prints the version of jaguar",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Version:\t", info.Version)
			fmt.Println("SDK version:\t", info.SDKVersion)
			fmt.Println("Build date:\t", info.Date)
		},
	}
	return cmd
}
