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
			shouldSkipPortCheck, err := cmd.Flags().GetBool("skip-port-check")
			if err != nil {
				return err
			}
			if !shouldSkipPortCheck {
				if port, err = CheckPort(port); err != nil {
					return err
				}
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
				// Make a temporary directory for the downloaded envelope.
				tmpDir, err := os.MkdirTemp("", "*-envelope")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)
				envelopePath, err = DownloadEnvelope(ctx, args[0], sdk.Version, tmpDir)
				if err != nil {
					return err
				}
			} else {
				envelopePath, err = GetCachedFirmwareEnvelopePath(ctx, sdk.Version, chip)
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

			uartEndpointOptions, err := getUartEndpointOptions(cmd)
			if err != nil {
				return err
			}

			bleEndpointOptions, err := getBLEEndpointOptions(cmd)
			if err != nil {
				return err
			}

			envelopeFile, err := BuildFirmwareEnvelope(ctx, envelopeOptions, deviceOptions, uartEndpointOptions, bleEndpointOptions)
			if err != nil {
				return err
			}
			defer os.Remove(envelopeFile.Name())

			flashArguments := []string{
				"flash",
				"--port", port,
				"--baud", strconv.Itoa(int(baud)),
			}

			// Golang equivalent of #ifdef Windows.  We skip this
			// because the whole uucp group issue does not affect
			// Windows, but on the other hand Windows has strange
			// escaping rules for COM ports over 10 (COM10, COM11),
			// which we don't want to deal with.
			if os.PathSeparator != '\\' && !shouldSkipPortCheck {
				// Use golang to check the port can be opened for writing first.
				// This is to avoid the error message from esptool.py, which is
				// confusing to users in the common case where the port is owned
				// by the dialout or uucp group.
				file, err := os.OpenFile(port, os.O_WRONLY, 0)
				if err != nil {
					return err
				}
				// Close the file again:
				file.Close()
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
	cmd.Flags().Bool("skip-port-check", false, "accept the given port without checking")
	cmd.Flags().Int("uart-endpoint-rx", -1, "add a UART endpoint to the device listening on the given pin")
	cmd.Flags().MarkHidden("uart-endpoint-rx")
	cmd.Flags().Uint("uart-endpoint-baud", 0, "set the baud rate for the UART endpoint")
	cmd.Flags().MarkHidden("uart-endpoint-baud")
	cmd.Flags().Bool("enable-ble", false, "enable BLE endpoint")
	return cmd
}
