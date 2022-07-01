// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

type binaryConfig struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Wifi struct {
		Password string `json:"password"`
		SSID     string `json:"ssid"`
	} `json:"wifi"`
}

func FirmwareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "firmware",
		Short: "Show or update firmware for a Jaguar device",
		Long: "Without the 'update' command show the firmware version for a Jaguar device.\n" +
			"The device reports the version information when it responds to pings.\n\n" +
			"With the 'update' command update the firmware of a Jaguar device via WiFi.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			fmt.Printf("Device '%s' is running Toit SDK %s\n", device.Name, device.SDKVersion)
			return nil
		},
	}
	cmd.AddCommand(FirmwareUpdateCmd())
	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	return cmd
}

func FirmwareUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the firmware on a Jaguar device",
		Long: "Update the firmware on a Jaguar device via WiFi. The device name and\n" +
			"id are preserved across the operation.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			var wifiSSID string
			if cmd.Flags().Changed("wifi-ssid") {
				wifiSSID, err = cmd.Flags().GetString("wifi-ssid")
				if err != nil {
					return err
				}
			} else if v, ok := os.LookupEnv(directory.WifiSSIDEnv); ok {
				wifiSSID = v
			} else {
				fmt.Printf("Enter WiFi network (SSID): ")
				wifiSSID, err = ReadLine()
				if err != nil {
					return err
				}
			}

			var wifiPassword string
			if cmd.Flags().Changed("wifi-password") {
				wifiPassword, err = cmd.Flags().GetString("wifi-password")
				if err != nil {
					return err
				}
			} else if v, ok := os.LookupEnv(directory.WifiPasswordEnv); ok {
				wifiPassword = v
			} else {
				fmt.Printf("Enter WiFi password for '%s': ", wifiSSID)
				pw, err := ReadPassword()
				if err != nil {
					fmt.Printf("\n")
					return err
				}
				wifiPassword = string(pw)
			}

			// We need to generate a new ID for the device, so entries in
			// the device flash stored by an older version are invalidated.
			newID := uuid.New().String()

			binTmpFile, err := BuildFirmwareImage(ctx, newID, device.Name, wifiSSID, wifiPassword)
			if err != nil {
				return err
			}
			defer os.Remove(binTmpFile.Name())

			bin, err := ioutil.ReadFile(binTmpFile.Name())
			if err != nil {
				return err
			}

			fmt.Printf("Updating firmware on '%s' to Toit SDK %s\n\n", device.Name, sdk.Version)
			if err := device.UpdateFirmware(ctx, sdk, bin); err != nil {
				return err
			}

			// Update the device ID and the SDK version and store them back, so users don't
			// have to scan and ping before they can use the device after the firmware update.
			// If the update failed or if the device got a new IP address after rebooting, we
			// will have to ping again.
			device.ID = newID
			device.SDKVersion = sdk.Version
			cfg.Set("device", device)
			return cfg.WriteConfig()
		},
	}

	// TODO(kasper): We really should be reusing the WiFi credentials.
	cmd.Flags().String("wifi-ssid", os.Getenv(directory.WifiSSIDEnv), "default WiFi SSID")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	return cmd
}

func BuildFirmwareImage(ctx context.Context, id string, name string, wifiSSID string, wifiPassword string) (*os.File, error) {
	sdk, err := GetSDK(ctx)
	if err != nil {
		return nil, err
	}

	esp32BinPath, err := directory.GetESP32ImagePath()
	if err != nil {
		return nil, err
	}

	configFile, err := os.CreateTemp("", "*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(configFile.Name())

	var config binaryConfig
	config.ID = id
	config.Name = name
	config.Wifi.SSID = wifiSSID
	config.Wifi.Password = wifiPassword
	if err := json.NewEncoder(configFile).Encode(config); err != nil {
		configFile.Close()
		return nil, err
	}
	configFile.Close()

	toitBin := filepath.Join(esp32BinPath, "toit.bin")

	binTmpFile, err := os.CreateTemp("", "*.bin")
	if err != nil {
		return nil, err
	}

	binFile, err := os.Open(toitBin)
	if err != nil {
		binTmpFile.Close()
		return nil, err
	}

	_, err = io.Copy(binTmpFile, binFile)
	binTmpFile.Close()
	binFile.Close()
	if err != nil {
		return nil, err
	}

	injectCmd := sdk.ToitRun(ctx, sdk.InjectConfigPath(), configFile.Name(), "--unique_id", id, binTmpFile.Name())
	injectCmd.Stderr = os.Stderr
	injectCmd.Stdout = os.Stdout
	if err := injectCmd.Run(); err != nil {
		return nil, err
	}
	return binTmpFile, nil
}
