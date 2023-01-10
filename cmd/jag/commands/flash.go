// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func FlashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flash [envelope]",
		Short: "Flash an ESP32 with the Jaguar firmware",
		Long: "Flash an ESP32 with the Jaguar firmware. The initial flashing is\n" +
			"done over a serial connection and it is used to give the ESP32 its initial\n" +
			"firmware and the necessary WiFi credentials.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			port, err := cmd.Flags().GetString("port")
			if err != nil {
				return err
			}
			if port, err = CheckPort(port); err != nil {
				return err
			}

			baud, err := cmd.Flags().GetUint("baud")
			if err != nil {
				return err
			}

			chip, err := cmd.Flags().GetString("chip")
			if err != nil {
				return err
			}

			if chip == "auto" {
				return fmt.Errorf("auto-detecting chip type isn't supported yet")
			}

			id := uuid.New()
			var name string
			if cmd.Flags().Changed("name") {
				name, err = cmd.Flags().GetString("name")
				if err != nil {
					return err
				}
			} else {
				name = GetRandomName(id[:])
			}

			wifiSSID, wifiPassword, err := getWifiCredentials(cmd)
			if err != nil {
				return err
			}

			deviceOptions := DeviceOptions{
				Id:           id.String(),
				Name:         name,
				Chip:         chip,
				WifiSsid:     wifiSSID,
				WifiPassword: wifiPassword,
			}

			var envelopePath string
			if len(args) == 1 {
				envelopePath = args[0]
			} else {
				envelopePath, err = directory.GetFirmwareEnvelopePath(chip)
				if err != nil {
					return err
				}
			}

			excludeJaguar, err := cmd.Flags().GetBool("exclude-jaguar")
			if err != nil {
				return err
			}

			envelopeOptions := EnvelopeOptions{
				Path:          envelopePath,
				ExcludeJaguar: excludeJaguar,
			}

			envelopeFile, err := BuildFirmwareEnvelope(ctx, envelopeOptions, deviceOptions)
			if err != nil {
				return err
			}
			defer os.Remove(envelopeFile.Name())

			flashArguments := []string{
				"flash",
				"--chip", chip,
				"--port", port,
				"--baud", strconv.Itoa(int(baud)),
			}

			fmt.Printf("Flashing device over serial on port '%s' ...\n", port)
			config := deviceOptions.GetConfig()
			return runFirmwareToolWithConfig(ctx, sdk, envelopeFile.Name(), config, flashArguments...)
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "serial port to flash via")
	cmd.Flags().Uint("baud", 921600, "baud rate used for the serial flashing")
	cmd.Flags().StringP("chip", "c", "esp32", "chip of the target device")
	cmd.Flags().String("wifi-ssid", "", "default WiFi network name")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().String("name", "", "name for the device, if not set a name will be auto generated")
	cmd.Flags().Bool("exclude-jaguar", false, "don't install the Jaguar service")
	return cmd
}
