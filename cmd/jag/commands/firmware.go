// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

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
			cfg, err := directory.GetDeviceConfig()
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
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
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
			cfg, err := directory.GetDeviceConfig()
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

			wifiSSID, wifiPassword, err := getWifiCredentials(cmd)
			if err != nil {
				return err
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

	cmd.Flags().String("wifi-ssid", "", "default WiFi network name")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
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

	envelopeInput := filepath.Join(esp32BinPath, "firmware.envelope")
	envelope, err := os.CreateTemp("", "*.envelope")
	if err != nil {
		return nil, err
	}
	defer envelope.Close()

	binTmpFile, err := os.CreateTemp("", "*.bin")
	if err != nil {
		return nil, err
	}

	jaguarSnapshot, err := directory.GetJaguarSnapshotPath()
	if err != nil {
		return nil, err
	}

	// TODO(kasper): Can we generate this in a nicer way?
	wifiProperties := "{ \"wifi.ssid\": \"" + wifiSSID + "\", \"wifi.password\": \"" + wifiPassword + "\" }"

	if err := RunFirmwareTool(ctx, sdk, envelopeInput, "container", "install", "-o", envelope.Name(), "jaguar", jaguarSnapshot); err != nil {
		return nil, err
	}
	if err := RunFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", "uuid", id); err != nil {
		return nil, err
	}
	if err := RunFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", "id", id); err != nil {
		return nil, err
	}
	if err := RunFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", "name", name); err != nil {
		return nil, err
	}
	if err := RunFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", "wifi", wifiProperties); err != nil {
		return nil, err
	}
	if err := RunFirmwareTool(ctx, sdk, envelope.Name(), "extract", "--binary", "-o", binTmpFile.Name()); err != nil {
		return nil, err
	}
	return binTmpFile, nil
}

func RunFirmwareTool(ctx context.Context, sdk *SDK, envelope string, args ...string) error {
	args = append([]string{"-e", envelope}, args...)
	cmd := sdk.Firmware(ctx, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
