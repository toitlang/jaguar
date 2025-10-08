// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

const (
	WifiCfgKey         = "wifi"
	WifiSSIDCfgKey     = "ssid"
	WifiPasswordCfgKey = "password"
	WifiNetworksCfgKey = "networks"
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

You can store multiple WiFi networks. Jaguar will try them in the
order they are listed.

Without any stored credentials, Jaguar will prompt for WiFi
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
				saveWifiCredentials(cfg, nil)
				return directory.WriteConfig(cfg)
			},
		},
	)
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "Lists stored WiFi credentials",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				cfg, err := directory.GetUserConfig()
				if err != nil {
					return err
				}
				creds := loadWifiCredentials(cfg)
				if len(creds) == 0 {
					fmt.Println("No stored WiFi credentials.")
					return nil
				}
				for idx, cred := range creds {
					password := "(empty)"
					if cred.Password != "" {
						maskedLength := len(cred.Password)
						if maskedLength > 8 {
							maskedLength = 8
						}
						password = strings.Repeat("*", maskedLength)
					}
					fmt.Printf("[%d] SSID: %s\tPassword: %s\n", idx+1, cred.SSID, password)
				}
				return nil
			},
		},
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Adds or updates WiFi credentials",
		Long: `Adds a WiFi network to the stored credentials.

If a network with the same SSID already exists, its password will be
updated. Otherwise, the network is added to the list.

Devices will try networks in the order they were added, prioritizing
networks that are currently visible during WiFi scanning.`,
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

			password, err := cmd.Flags().GetString("wifi-password")
			if err != nil {
				return err
			}

			creds := loadWifiCredentials(cfg)
			creds = upsertWifiCredential(creds, wifiCredential{SSID: ssid, Password: password})
			saveWifiCredentials(cfg, creds)
			return directory.WriteConfig(cfg)
		},
	}
	addCmd.Flags().String("wifi-ssid", os.Getenv(directory.WifiSSIDEnv), "WiFi network name")
	addCmd.Flags().String("wifi-password", os.Getenv(directory.WifiPasswordEnv), "WiFi password")
	addCmd.MarkFlagRequired("wifi-ssid")
	addCmd.MarkFlagRequired("wifi-password")
	cmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Removes stored WiFi credentials",
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

			creds := loadWifiCredentials(cfg)
			updated, removed := removeWifiCredential(creds, ssid)
			if !removed {
				return fmt.Errorf("wifi network '%s' not found", strings.TrimSpace(ssid))
			}
			saveWifiCredentials(cfg, updated)
			return directory.WriteConfig(cfg)
		},
	}
	removeCmd.Flags().String("wifi-ssid", "", "WiFi network name to remove")
	removeCmd.MarkFlagRequired("wifi-ssid")
	cmd.AddCommand(removeCmd)

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Sets a single WiFi network (replaces all existing entries)",
		Long: `Sets WiFi credentials and replaces any previously stored networks.

This command clears all existing WiFi networks and stores only the
provided SSID and password. For backward compatibility, this matches
the behavior of the previous WiFi configuration system.

To add a network without removing existing ones, use 'jag config wifi add'.`,
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

			pass, err := cmd.Flags().GetString("wifi-password")
			if err != nil {
				return err
			}
			saveWifiCredentials(cfg, []wifiCredential{{SSID: ssid, Password: pass}})
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
