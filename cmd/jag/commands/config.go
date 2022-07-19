// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

const (
	WifiCfgKey         = "wifi"
	WifiSSIDCfgKey     = "ssid"
	WifiPasswordCfgKey = "password"
)

func ConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure Jaguar",
	}

	cmd.AddCommand(
		ConfigAnalyticsCmd(),
		ConfigWifiCmd(),
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

func ConfigWifiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wifi",
		Short: "Configure the WiFi settings for Jaguar devices",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "clear",
			Short: "Deletes the stored WiFi credentials",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				cfg, err := directory.GetUserConfig()
				if err != nil {
					return err
				}
				if cfg.IsSet(WifiCfgKey + "." + WifiSSIDCfgKey) {
					delete(cfg.Get(WifiCfgKey).(map[string]interface{}), WifiSSIDCfgKey)
				}
				if cfg.IsSet(WifiCfgKey + "." + WifiPasswordCfgKey) {
					delete(cfg.Get(WifiCfgKey).(map[string]interface{}), WifiPasswordCfgKey)
				}
				return directory.WriteConfig(cfg)
			},
		},
	)

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Sets the WiFi SSID and password",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := directory.GetUserConfig()
			if err != nil {
				return err
			}

			ssid, err := cmd.Flags().GetString("wifi-ssid")
			if err != nil {
				return err
			}
			cfg.Set(WifiCfgKey+"."+WifiSSIDCfgKey, ssid)

			pass, err := cmd.Flags().GetString("wifi-password")
			if err != nil {
				return err
			}
			cfg.Set(WifiCfgKey+"."+WifiPasswordCfgKey, pass)

			return directory.WriteConfig(cfg)
		},
	}
	setCmd.Flags().String("wifi-ssid", os.Getenv(directory.WifiSSIDEnv), "default WiFi SSID")
	setCmd.Flags().String("wifi-password", os.Getenv(directory.WifiPasswordEnv), "default WiFi password")
	setCmd.MarkFlagRequired("wifi-ssid")
	setCmd.MarkFlagRequired("wifi-password")
	cmd.AddCommand(setCmd)
	return cmd
}

func configAnalytics(disable bool) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		cfg, err := directory.GetUserConfig()
		if err != nil {
			return err
		}

		cfg.Set("analytics.disabled", disable)
		return directory.WriteConfig(cfg)
	}
}
