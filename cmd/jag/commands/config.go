// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

const (
	WifiCfgKey         = "wifi"
	WifiSSIDCfgKey     = "ssid"
	WifiPasswordCfgKey = "password"
)

func ConfigCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure Jaguar",
		Long:  "Configure the Jaguar command line tool.",
	}

	cmd.AddCommand(
		ConfigAnalyticsCmd(),
		ConfigUpToDateCmd(info),
		ConfigWifiCmd(),
	)
	return cmd
}

func ConfigAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Configure reporting of anonymous tool usage statistics and crash reports",
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

func ConfigUpToDateCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up-to-date",
		Short: "Configure periodic up-to-date checks for the Jaguar command line tool",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "enable",
			Short: "Enable periodic up-to-date checks",
			Args:  cobra.NoArgs,
			RunE:  configUpToDate(info, false),
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Disable periodic up-to-date checks",
			Args:  cobra.NoArgs,
			RunE:  configUpToDate(info, true),
		},
	)
	return cmd
}

func ConfigWifiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wifi",
		Short: "Configure the default WiFi settings for Jaguar devices",
		Long: `Sets the default WiFi credentials for Jaguar devices.

When Jaguar flashes a device ('jag flash'), or updates the firmware
('jag firmware update'), then it will use the stored credentials.

Without any stored credentials, Jaguar will prompt for the WiFi
credentials whenever necessary.`,
		Args: cobra.NoArgs,
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
	setCmd.Flags().String("wifi-ssid", os.Getenv(directory.WifiSSIDEnv), "default WiFi network name")
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

		if disable {
			var res UpdateToDate
			disabled := false
			if cfg.IsSet(UpToDateKey) {
				if err := cfg.UnmarshalKey(UpToDateKey, &res); err == nil {
					disabled = res.Disabled
				}
			}

			if !disabled {
				cfg.Set(UpToDateKey+".disabled", true)
				fmt.Println("Also turning off periodic up-to-date checks. You can renable")
				fmt.Println("these through:")
				fmt.Println()
				fmt.Println("  $ jag config up-to-date enable")
				fmt.Println()
			}
		}

		cfg.Set("analytics.disabled", disable)
		return directory.WriteConfig(cfg)
	}
}

func configUpToDate(info Info, disable bool) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		cfg, err := directory.GetUserConfig()
		if err != nil {
			return err
		}

		cfg.Set(UpToDateKey+".disabled", disable)
		if err := directory.WriteConfig(cfg); err != nil {
			return err
		}

		if !disable {
			CheckUpToDate(info)
		}
		return nil
	}
}
