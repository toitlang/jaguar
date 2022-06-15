// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

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
