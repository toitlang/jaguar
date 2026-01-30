// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

type callback func(id string, envelopeFile *os.File, config map[string]interface{}) error

func addFirmwareFlashFlags(cmd *cobra.Command, defaultChip string, nameHelp string) {
	cmd.Flags().String("name", "", nameHelp)
	cmd.Flags().StringP("chip", "c", defaultChip, "chip of the target device")
	cmd.Flags().String("wifi-ssid", "", "default WiFi network name")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().Bool("exclude-jaguar", false, "don't install the Jaguar service")
	cmd.Flags().Int("uart-endpoint-rx", -1, "add a UART endpoint to the device listening on the given pin")
	cmd.Flags().MarkHidden("uart-endpoint-rx")
	cmd.Flags().Uint("uart-endpoint-baud", 0, "set the baud rate for the UART endpoint")
	cmd.Flags().MarkHidden("uart-endpoint-baud")
}

func withFirmware(cmd *cobra.Command, args []string, device Device, fun callback) error {
	ctx := cmd.Context()

	sdk, err := GetSDK(ctx)
	if err != nil {
		return err
	}

	chip, err := cmd.Flags().GetString("chip")
	if err != nil {
		return err
	}

	if chip == "auto" || chip == "" {
		if device != nil {
			chip = device.Chip()
		} else {
			return fmt.Errorf("chip type must be specified")
		}
	}

	wifiNetworks, err := getWifiCredentials(cmd)
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
	} else if device != nil {
		name = device.Name()
	} else {
		name = GetRandomName(id[:])
	}

	deviceOptions := DeviceOptions{
		Id:           id.String(),
		Name:         name,
		Chip:         chip,
		WifiNetworks: wifiNetworks,
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

	envelopeFile, err := BuildFirmwareEnvelope(ctx, envelopeOptions, deviceOptions, uartEndpointOptions)
	if err != nil {
		return err
	}
	defer os.Remove(envelopeFile.Name())

	config := deviceOptions.GetConfig()

	return fun(id.String(), envelopeFile, config)
}

func BuildFirmwareEnvelope(ctx context.Context, envelope EnvelopeOptions, device DeviceOptions, uartEndpointOptions map[string]interface{}) (*os.File, error) {
	sdk, err := GetSDK(ctx)
	if err != nil {
		return nil, err
	}

	envelopeFile, err := os.CreateTemp("", "*.envelope")
	if err != nil {
		return nil, err
	}
	defer envelopeFile.Close()

	if envelope.ExcludeJaguar {
		source, err := os.Open(envelope.Path)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(envelopeFile, source)
		source.Close()
		if err != nil {
			return nil, err
		}
	} else {
		jaguarSnapshot, err := directory.GetJaguarSnapshotPath()
		if err != nil {
			return nil, err
		}

		configAssetMap := map[string]interface{}{
			"id":   device.Id,
			"name": device.Name,
			"chip": device.Chip,
		}
		if uartEndpointOptions != nil {
			configAssetMap["endpointUart"] = uartEndpointOptions
		}
		// Add WiFi configuration to the asset config so Jaguar can read it
		if len(device.WifiNetworks) > 0 {
			wifiConfig := make(map[string]interface{})
			// Add the first network as the default for backward compatibility
			first := device.WifiNetworks[0]
			wifiConfig[WifiSSIDCfgKey] = first.SSID
			wifiConfig[WifiPasswordCfgKey] = first.Password

			// Add all networks in the networks list
			networks := make([]map[string]string, 0, len(device.WifiNetworks))
			for _, cred := range device.WifiNetworks {
				networks = append(networks, map[string]string{
					WifiSSIDCfgKey:     cred.SSID,
					WifiPasswordCfgKey: cred.Password,
				})
			}
			wifiConfig[WifiNetworksCfgKey] = networks
			configAssetMap[WifiCfgKey] = wifiConfig
		}
		configAssetJson, err := json.Marshal(configAssetMap)
		if err != nil {
			return nil, err
		}

		configAssetJsonFile, err := os.CreateTemp("", "*.json.assets")
		if err != nil {
			return nil, err
		}
		defer configAssetJsonFile.Close()

		if err := os.WriteFile(configAssetJsonFile.Name(), configAssetJson, 0666); err != nil {
			return nil, err
		}

		assetsFile, err := os.CreateTemp("", "*.assets")
		if err != nil {
			return nil, err
		}
		defer assetsFile.Close()

		if err := runAssetsTool(ctx, sdk, assetsFile.Name(), "create"); err != nil {
			return nil, err
		}

		if err := runAssetsTool(ctx, sdk, assetsFile.Name(), "add", "--format=tison", "config", configAssetJsonFile.Name()); err != nil {
			return nil, err
		}

		if err := runFirmwareTool(ctx, sdk, envelope.Path,
			"container", "install", "-o", envelopeFile.Name(),
			"--assets", assetsFile.Name(),
			"--trigger=boot", "--critical",
			"jaguar", jaguarSnapshot); err != nil {
			return nil, err
		}
	}

	if err := setFirmwareProperty(ctx, sdk, envelopeFile, "uuid", device.Id); err != nil {
		return nil, err
	}

	if err := copySnapshotsIntoCache(ctx, sdk, envelopeFile); err != nil {
		return nil, err
	}

	return envelopeFile, nil
}

type DeviceOptions struct {
	Id           string
	Name         string
	Chip         string
	WifiNetworks []wifiCredential
}

type EnvelopeOptions struct {
	Path          string
	ExcludeJaguar bool
}

func (d DeviceOptions) GetConfig() map[string]interface{} {
	if len(d.WifiNetworks) == 0 {
		return nil
	}

	first := d.WifiNetworks[0]
	wifiConfig := map[string]interface{}{
		// Legacy format for backward compatibility
		WifiSSIDCfgKey:     first.SSID,
		WifiPasswordCfgKey: first.Password,
	}

	// Always include the networks array for new firmware
	networks := make([]map[string]string, 0, len(d.WifiNetworks))
	for _, cred := range d.WifiNetworks {
		networks = append(networks, map[string]string{
			WifiSSIDCfgKey:     cred.SSID,
			WifiPasswordCfgKey: cred.Password,
		})
	}
	wifiConfig[WifiNetworksCfgKey] = networks

	return map[string]interface{}{
		WifiCfgKey: wifiConfig,
	}
}

func ExtractFirmwareBin(ctx context.Context, sdk *SDK, envelopePath string, config map[string]interface{}) (*os.File, error) {
	binaryFile, err := os.CreateTemp("", "firmware.bin.*")
	if err != nil {
		return nil, err
	}

	arguments := []string{
		"extract",
		"--format=binary",
		"-o", binaryFile.Name(),
	}

	if err := runFirmwareToolWithConfig(ctx, sdk, envelopePath, config, arguments...); err != nil {
		binaryFile.Close()
		return nil, err
	}
	return binaryFile, nil
}

func ExtractFirmware(ctx context.Context, sdk *SDK, envelopePath string, format string, config map[string]interface{}) (*os.File, error) {
	outputFile, err := os.CreateTemp("", "firmware-"+format+".*")
	if err != nil {
		return nil, err
	}
	if err := runFirmwareToolWithConfig(ctx, sdk, envelopePath, config, "extract", "--format", format, "-o", outputFile.Name()); err != nil {
		outputFile.Close()
		return nil, err
	}
	return outputFile, nil
}

func setFirmwareProperty(ctx context.Context, sdk *SDK, envelope *os.File, key string, value string) error {
	return runFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", key, value)
}

func runFirmwareToolWithConfig(ctx context.Context, sdk *SDK, envelopePath string, config map[string]interface{}, args ...string) error {
	if config != nil {
		configFile, err := os.CreateTemp("", "*.json.config")
		if err != nil {
			return err
		}
		defer os.Remove(configFile.Name())

		configBytes, err := json.Marshal(config)
		if err != nil {
			return err
		}

		if err := os.WriteFile(configFile.Name(), configBytes, 0666); err != nil {
			return err
		}

		args = append(args, "--config", configFile.Name())
	}
	return runFirmwareTool(ctx, sdk, envelopePath, args...)
}

func runFirmwareTool(ctx context.Context, sdk *SDK, envelopePath string, args ...string) error {
	args = append([]string{"-e", envelopePath}, args...)
	cmd := sdk.FirmwareTool(ctx, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func copySnapshotsIntoCache(ctx context.Context, sdk *SDK, envelope *os.File) error {
	listFile, err := os.CreateTemp("", "firmware-list.*")
	if err != nil {
		return err
	}
	defer os.Remove(listFile.Name())

	if err := runFirmwareTool(ctx, sdk, envelope.Name(), "container", "list", "--output-format", "json", "-o", listFile.Name()); err != nil {
		return err
	}

	listBytes, err := os.ReadFile(listFile.Name())
	if err != nil {
		return err
	}

	var entries map[string]map[string]interface{}
	if err := json.Unmarshal(listBytes, &entries); err != nil {
		return err
	}

	for name, entry := range entries {
		kind := entry["kind"]
		if kind != "snapshot" {
			continue
		}

		snapshotFile, err := os.CreateTemp("", "firmware-snapshot.*")
		if err != nil {
			return err
		}
		defer os.Remove(snapshotFile.Name())

		snapshotExtractArguments := []string{
			"container", "extract",
			"-o", snapshotFile.Name(),
			"--part=image",
			name,
		}
		if err := runFirmwareTool(ctx, sdk, envelope.Name(), snapshotExtractArguments...); err != nil {
			return err
		}

		if err := copySnapshotIntoCache(snapshotFile.Name()); err != nil {
			return err
		}
	}
	return nil
}

func copySnapshotIntoCache(path string) error {
	uuid, err := GetUuid(path)
	if err != nil {
		return err
	}

	stateDirectory, err := directory.GetSnapshotsStatePath()
	if err != nil {
		return err
	}

	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(filepath.Join(stateDirectory, uuid.String()+".snapshot"))
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
