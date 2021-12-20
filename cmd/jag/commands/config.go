// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func ConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure jaguar",
	}

	cmd.AddCommand(
		ConfigAnalyticsCmd(),
	)
	return cmd
}

func ConfigAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Configure reporting of anonymous tool usage statistics and crash reports.",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "enable",
			Short: "Enable reporting of anonymous tool usage statistics and crash reports",
			Args:  cobra.NoArgs,
			RunE:  configAnalytics(false),
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Disable reporting of anonymous tool usage statistics and crash reports",
			Args:  cobra.NoArgs,
			RunE:  configAnalytics(true),
		},
	)
	return cmd
}

func configAnalytics(disable bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := directory.GetUserConfig()
		if err != nil {
			return err
		}

		cfg.Set("analytics.disabled", disable)
		return directory.WriteConfig(cfg)
	}
}
