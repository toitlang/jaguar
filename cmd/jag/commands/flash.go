// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func FlashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flash",
		Short: "Flash an ESP32 with the Jaguar image",
		Long: "Flash an ESP32 with the Jaguar application image. The initial flashing is\n" +
			"done over a serial connection and it is used to give the ESP32 its initial\n" +
			"firmware and the necessary WiFi credentials.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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

			esptoolPath, err := directory.GetEsptoolPath()
			if err != nil {
				return err
			}

			esp32BinPath, err := directory.GetESP32ImagePath()
			if err != nil {
				return err
			}

			binTmpFile, err := BuildFirmwareImage(ctx, id.String(), name, wifiSSID, wifiPassword)
			if err != nil {
				return err
			}
			defer os.Remove(binTmpFile.Name())

			// Create a file with zap bytes (0xff) for clearing the OTA data partition.
			zappedOtaDataFile, err := os.CreateTemp("", "*.otadata")
			if err != nil {
				return err
			}
			defer os.Remove(zappedOtaDataFile.Name())

			_, err = zappedOtaDataFile.Write(bytes.Repeat([]byte{0xff}, 0x2000))
			if err != nil {
				return err
			}
			zappedOtaDataFile.Close()

			flashArgs := []string{
				"--chip", "esp32", "--port", port, "--baud", strconv.Itoa(int(baud)), "--before", "default_reset", "--after", "hard_reset", "write_flash", "-z", "--flash_mode", "dio",
				"--flash_freq", "40m", "--flash_size", "detect",
				"0x001000", filepath.Join(esp32BinPath, "bootloader", "bootloader.bin"),
				"0x008000", filepath.Join(esp32BinPath, "partitions.bin"),
				"0x00d000", zappedOtaDataFile.Name(), // Force bootloader to boot from OTA 0.
				"0x010000", binTmpFile.Name(),
			}

			fmt.Printf("Flashing device over serial on port '%s' ...\n", port)
			flashCmd := exec.CommandContext(ctx, esptoolPath, flashArgs...)
			flashCmd.Stderr = os.Stderr
			flashCmd.Stdout = os.Stdout
			return flashCmd.Run()
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "serial port to flash via")
	cmd.Flags().Uint("baud", 921600, "baud rate used for the serial flashing")
	cmd.Flags().String("wifi-ssid", os.Getenv(directory.WifiSSIDEnv), "default WiFi SSID")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().String("name", "", "name for the device, if not set a name will be auto generated")
	return cmd
}
