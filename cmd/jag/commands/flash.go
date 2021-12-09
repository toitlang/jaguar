// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

type binaryConfig struct {
	Wifi struct {
		Password string `json:"password"`
		SSID     string `json:"ssid"`
	} `json:"wifi"`
}

func FlashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "flash",
		Short:        "flash a device with the jaguar image",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			port, err := cmd.Flags().GetString("port")
			if err != nil {
				return err
			}

			baud, err := cmd.Flags().GetUint("baud")
			if err != nil {
				return err
			}

			wifiSsid, err := cmd.Flags().GetString("wifi-ssid")
			if err != nil {
				return err
			}

			wifiPassword, err := cmd.Flags().GetString("wifi-password")
			if err != nil {
				return err
			}

			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			esptoolPath, ok := os.LookupEnv(EsptoolPathEnv)
			if !ok {
				return fmt.Errorf("you must set the env variable '%s'", EsptoolPathEnv)
			}

			esp32BinPath, ok := os.LookupEnv(ESP32BinEnv)
			if !ok {
				return fmt.Errorf("you must set the env variable '%s'", ESP32BinEnv)
			}

			configFile, err := os.CreateTemp("", "*.json")
			if err != nil {
				return err
			}
			defer os.Remove(configFile.Name())

			var config binaryConfig
			config.Wifi.Password = wifiPassword
			config.Wifi.SSID = wifiSsid
			if err := json.NewEncoder(configFile).Encode(config); err != nil {
				configFile.Close()
				return err
			}
			configFile.Close()

			toitBin := filepath.Join(esp32BinPath, "toit.bin")
			injectCmd := sdk.Toitvm(ctx, sdk.InjectConfigPath(), configFile.Name(), toitBin)
			injectCmd.Stderr = os.Stderr
			injectCmd.Stdout = os.Stdout
			if err := injectCmd.Run(); err != nil {
				return err
			}

			flashArgs := []string{
				"--chip", "esp32", "--port", port, "--baud", strconv.Itoa(int(baud)), "--before", "default_reset", "--after", "hard_reset", "write_flash", "-z", "--flash_mode", "dio",
				"--flash_freq", "40m", "--flash_size", "detect",
				"0xd000", filepath.Join(esp32BinPath, "ota_data_initial.bin"),
				"0x1000", filepath.Join(esp32BinPath, "/bootloader/bootloader.bin"),
				"0x10000", toitBin,
				"0x8000", filepath.Join(esp32BinPath, "partitions.bin"),
			}

			flashCmd := exec.CommandContext(ctx, esptoolPath, flashArgs...)
			flashCmd.Stderr = os.Stderr
			flashCmd.Stdout = os.Stdout
			return flashCmd.Run()
		},
	}

	cmd.Flags().StringP("port", "p", "/dev/ttyUSB0", "port to flash onto")
	cmd.Flags().Uint("baud", 921600, "the baud rate to flash with")
	cmd.Flags().String("wifi-ssid", "", "The WiFi SSID to flash with")
	cmd.Flags().String("wifi-password", "", "The WiFi Password to flash with")
	cmd.MarkFlagRequired("wifi-ssid")
	cmd.MarkFlagRequired("wifi-password")
	return cmd
}
