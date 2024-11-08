// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"

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
			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			fmt.Printf("Device '%s' is running Toit SDK %s\n", device.Name(), device.SDKVersion())
			return nil
		},
	}
	cmd.AddCommand(FirmwareUpdateCmd())
	cmd.AddCommand(FirmwareExtractCmd())
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	return cmd
}

func FirmwareUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "update [envelope]",
		Short:        "Update the firmware on a Jaguar device",
		Long:         "Update the firmware on a Jaguar device via WiFi",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			// We get a new ID for the device, so entries in the device flash stored
			// by an older version are invalidated.
			return withFirmware(cmd, args, device, func(newID string, envelopeFile *os.File, config map[string]interface{}) error {

				firmwareBin, err := ExtractFirmwareBin(ctx, sdk, envelopeFile.Name(), config)
				if err != nil {
					return err
				}
				defer os.Remove(firmwareBin.Name())

				bin, err := os.ReadFile(firmwareBin.Name())
				if err != nil {
					return err
				}

				fmt.Printf("Updating firmware on '%s' to Toit SDK %s\n\n", device.Name(), sdk.Version)
				if err := device.UpdateFirmware(ctx, sdk, bin); err != nil {
					return err
				}

				// Update the device ID and the SDK version and store them back, so users don't
				// have to scan and ping before they can use the device after the firmware update.
				// If the update failed or if the device got a new IP address after rebooting, we
				// will have to ping again.
				device.SetID(newID)
				device.SetSDKVersion(sdk.Version)
				deviceCfg, err := directory.GetDeviceConfig()
				if err != nil {
					return err
				}
				deviceCfg.Set("device", device.ToJson())
				return deviceCfg.WriteConfig()
			})
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	addFirmwareFlashFlags(cmd, "", "new name of the device, if given")
	return cmd
}

func FirmwareExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract [envelope]",
		Short: "Build the firmware image of a Jaguar device",
		Long: "Build the firmware image of a Jaguar device, for flashing, Wokwi, or QEMU." +
			"\n" +
			"Wokwi\n" +
			"-----\n" +
			"Remember to use the WiFi SSID \"Wokwi-GUEST\" without a password.\n" +
			"You need a Club subscription to be able to connect to your device, but\n" +
			"the device is able to reach the Internet without a subscription.\n" +
			"\n" +
			"To run the image go to https://wokwi.com/projects/new/esp32, then\n" +
			"press F1 and run the command \"Upload Firmware and Start Simulation\".\n",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			output, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}
			if output == "" {
				return fmt.Errorf("missing output file")
			}

			return withFirmware(cmd, args, nil, func(newID string, envelopeFile *os.File, config map[string]interface{}) error {

				sdk, err := GetSDK(ctx)
				if err != nil {
					return err
				}

				imageFile, err := ExtractFirmware(ctx, sdk, envelopeFile.Name(), "image", config)
				if err != nil {
					return err
				}
				defer os.Remove(imageFile.Name())

				imageBin, err := os.ReadFile(imageFile.Name())
				if err != nil {
					return err
				}

				return os.WriteFile(output, imageBin, 0644)
			})
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	addFirmwareFlashFlags(cmd, "esp32", "name of the device")
	cmd.Flags().StringP("output", "o", "", "write the firmware image to a file")
	return cmd
}
