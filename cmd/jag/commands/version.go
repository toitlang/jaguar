// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func VersionCmd(info Info, isReleaseBuild bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Prints the version of Jaguar",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			version := "---"
			sdkVersion := "unknown"
			buildDate := "---"

			if isReleaseBuild {
				version = info.Version
				sdkVersion = info.SDKVersion
				buildDate = info.Date
			} else {
				ctx := cmd.Context()
				sdk, _ := GetSDK(ctx)
				if sdk.Version != "" {
					sdkVersion = sdk.Version
				}
			}

			fmt.Println("Version:\t", version)
			fmt.Println("SDK version:\t", sdkVersion)
			fmt.Println("Build date:\t", buildDate)
		},
	}
	return cmd
}
