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

	"github.com/hashicorp/go-version"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

type callback func(id string, envelopeFile *os.File, config map[string]interface{}) error
type probeChip func(ctx context.Context, sdk *SDK) (string, error)

func addFirmwareFlashFlags(cmd *cobra.Command, nameHelp string) {
	cmd.Flags().String("name", "", nameHelp)
	cmd.Flags().StringP("chip", "c", "auto", "chip of the target device")
	cmd.Flags().String("wifi-ssid", "", "default WiFi network name")
	cmd.Flags().String("wifi-password", "", "default WiFi password")
	cmd.Flags().Bool("exclude-jaguar", false, "don't install the Jaguar service")
	cmd.Flags().Int("uart-endpoint-rx", -1, "add a UART endpoint to the device listening on the given pin")
	cmd.Flags().MarkHidden("uart-endpoint-rx")
	cmd.Flags().Uint("uart-endpoint-baud", 0, "set the baud rate for the UART endpoint")
	cmd.Flags().MarkHidden("uart-endpoint-baud")
}

func withFirmware(cmd *cobra.Command, args []string, probeChip probeChip, device Device, fun callback) error {
	ctx := cmd.Context()

	info := GetInfo(ctx)
	jagVersion := info.Version

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
		} else if probeChip != nil {
			// Run the esptool to probe the chip type.
			probedChip, err := probeChip(ctx, sdk)
			if err != nil {
				return fmt.Errorf("failed to probe chip type: %w", err)
			} else {
				fmt.Printf("Probed chip type: %s\n", probedChip)
			}
			chip = probedChip
		} else {
			chip = ""
			// We must have an envelope path to get the chip type from.
			if len(args) != 1 {
				return fmt.Errorf("chip type must be specified when no envelope is given")
			}
		}
	}

	wifiSSID, wifiPassword, err := getWifiCredentials(cmd)
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

	var envelopePath string
	if len(args) == 1 {
		// Make a temporary directory for the downloaded envelope.
		tmpDir, err := os.MkdirTemp("", "*-envelope")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		envelopePath, err = DownloadEnvelope(ctx, args[0], jagVersion, sdk.Version, tmpDir)
	} else {
		envelopePath, err = GetCachedFirmwareEnvelopePath(ctx, jagVersion, sdk.Version, chip)
		if err != nil {
			return err
		}
	}

	envelopeChip, err := GetFirmwareChip(ctx, sdk, envelopePath)
	if err != nil {
		return fmt.Errorf("failed to get chip type from envelope: %w", err)
	}

	if chip != "" {
		if chip != envelopeChip {
			isError := true
			if device != nil {
				// Older versions of Jaguar (using SDK versions prior to v2.0.0-alpha.189) didn't
				// set the chip type correctly (and used "esp32" for all ESP32 variants). We allow
				// firmware updating in that case, but warn the user.
				deviceVersion, err := version.NewVersion(device.SDKVersion())
				if err != nil {
					return fmt.Errorf("failed to parse device SDK version '%s': %w", device.SDKVersion(), err)
				}
				correctedVersion, err := version.NewVersion("v2.0.0-alpha.189")
				if err != nil {
					return fmt.Errorf("failed to parse corrected version: %w", err)
				}
				if chip == "esp32" && deviceVersion.LessThan(correctedVersion) {
					fmt.Fprintf(os.Stderr, "warning: device chip type is 'esp32', assuming it is compatible with envelope chip '%s'\n", envelopeChip)
					// Set the chip to the envelope chip to fix the mistake on the device.
					chip = envelopeChip
					isError = false
				}
			}
			if isError {
				return fmt.Errorf("chip type mismatch: expected '%s', but envelope is for '%s'", chip, envelopeChip)
			}
		}
	} else {
		chip = envelopeChip
	}

	deviceOptions := DeviceOptions{
		Id:           id.String(),
		Name:         name,
		Chip:         chip,
		WifiSsid:     wifiSSID,
		WifiPassword: wifiPassword,
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
	info := GetInfo(ctx)
	jagVersion := info.Version

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
		jaguarSnapshot, err := directory.GetJaguarSnapshotPath(jagVersion)
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

func GetFirmwareChip(ctx context.Context, sdk *SDK, envelopePath string) (string, error) {
	// Run the firmware tool with `show -e <envelopePath> --output-format json` and parse the
	// output ("chip" field).

	execCmd := sdk.FirmwareTool(ctx, "show", "-e", envelopePath, "--output-format", "json")
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get firmware chip type: %w: %s", err, string(output))
	}

	var info map[string]interface{}
	if err := json.Unmarshal(output, &info); err != nil {
		return "", fmt.Errorf("failed to parse firmware info: %w", err)
	}

	chip, ok := info["chip"].(string)
	if !ok {
		return "", fmt.Errorf("failed to get chip type from firmware info")
	}

	return chip, nil
}

type DeviceOptions struct {
	Id           string
	Name         string
	Chip         string
	WifiSsid     string
	WifiPassword string
}

type EnvelopeOptions struct {
	Path          string
	ExcludeJaguar bool
}

func (d DeviceOptions) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"wifi": map[string]string{
			"wifi.ssid":     d.WifiSsid,
			"wifi.password": d.WifiPassword,
		},
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
