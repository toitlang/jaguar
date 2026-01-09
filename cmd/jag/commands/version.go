// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func VersionCmd(info Info, isReleaseBuild bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Print the version of Jaguar",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			jaguarVersion := "---"
			sdkVersion := "unknown"
			buildDate := info.Date

			if isReleaseBuild {
				jaguarVersion = info.Version
				sdkVersion = info.SDKVersion
			} else {
				// Development build: try to get Jaguar version from git
				jaguarVersion = getGitVersion()

				// Get live SDK version
				ctx := cmd.Context()
				sdk, err := GetSDK(ctx)
				if err != nil {
					fmt.Printf("Warning: Could not retrieve SDK version: %v\n", err)
				} else if sdk != nil && sdk.Version != "" {
					sdkVersion = sdk.Version
				}
			}

			fmt.Printf("Jaguar version:\t%s\n", jaguarVersion)
			fmt.Printf("SDK version:\t%s\n", sdkVersion)
			fmt.Printf("Build date:\t%s\n", buildDate)

			if !isReleaseBuild {
				fmt.Println("Build type:\tdevelopment")
			}
		},
	}
	return cmd
}

// getGitVersion tries to determine a useful version string from git
func getGitVersion() string {
	// First, try to get the exact tag (e.g., v2.1.0)
	if tag, err := exec.Command("git", "describe", "--tags", "--exact-match").Output(); err == nil {
		return strings.TrimSpace(string(tag))
	}

	// Then, try describe with --tags (e.g., v2.1.0-5-gabc123)
	if desc, err := exec.Command("git", "describe", "--tags", "--dirty").Output(); err == nil {
		return strings.TrimSpace(string(desc))
	}

	// Fallback: short commit hash
	if rev, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		commit := strings.TrimSpace(string(rev))
		return "dev-" + commit
	}

	return "dev-unknown"
}
