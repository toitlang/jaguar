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
		Use:   "update [envelope]",
		Short: "Update the firmware on a Jaguar device",
		Long: "Update the firmware on a Jaguar device via WiFi. The device name and\n" +
			"id are preserved across the operation.",
		Args:         cobra.MaximumNArgs(1),
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

			// We need to generate a new ID for the device, so entries in
			// the device flash stored by an older version are invalidated.
			newID := uuid.New().String()

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			wifiSSID, wifiPassword, err := getWifiCredentials(cmd)
			if err != nil {
				return err
			}

			deviceOptions := DeviceOptions{
				Id:           newID,
				Name:         device.Name,
				WifiSsid:     wifiSSID,
				WifiPassword: wifiPassword,
			}

			var envelopePath string
			if len(args) == 1 {
				envelopePath = args[0]
			} else {
				envelopePath, err = directory.GetFirmwareEnvelopePath("esp32")
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

			config := deviceOptions.GetConfig()
			firmwareBin, err := ExtractFirmware(ctx, sdk, envelopeFile.Name(), config)
			if err != nil {
				return err
			}
			defer os.Remove(firmwareBin.Name())

			bin, err := ioutil.ReadFile(firmwareBin.Name())
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
	cmd.Flags().Bool("exclude-jaguar", false, "don't install the Jaguar service")
	return cmd
}

type DeviceOptions struct {
	Id           string
	Name         string
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

func BuildFirmwareEnvelope(ctx context.Context, envelope EnvelopeOptions, device DeviceOptions) (*os.File, error) {
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

		if err := runFirmwareTool(ctx, sdk, envelope.Path, "container", "install", "--assets", assetsFile.Name(), "-o", envelopeFile.Name(), "jaguar", jaguarSnapshot); err != nil {
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

func ExtractFirmware(ctx context.Context, sdk *SDK, envelopePath string, config map[string]interface{}) (*os.File, error) {
	configFile, err := os.CreateTemp("", "*.json.config")
	if err != nil {
		return nil, err
	}
	defer os.Remove(configFile.Name())

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(configFile.Name(), configBytes, 0666); err != nil {
		return nil, err
	}

	binaryFile, err := os.CreateTemp("", "firmware.bin.*")
	if err != nil {
		return nil, err
	}

	arguments := []string{
		"extract",
		"--format=binary",
		"-o", binaryFile.Name(),
		"--config", configFile.Name(),
	}

	if err := runFirmwareTool(ctx, sdk, envelopePath, arguments...); err != nil {
		binaryFile.Close()
		return nil, err
	}
	return binaryFile, nil
}

func ExtractFirmwarePart(ctx context.Context, sdk *SDK, envelopePath string, part string) (*os.File, error) {
	partFile, err := os.CreateTemp("", part+".*")
	if err != nil {
		return nil, err
	}
	if err := runFirmwareTool(ctx, sdk, envelopePath, "extract", "--"+part, "-o", partFile.Name()); err != nil {
		partFile.Close()
		return nil, err
	}
	return partFile, nil
}

func setFirmwareProperty(ctx context.Context, sdk *SDK, envelope *os.File, key string, value string) error {
	return runFirmwareTool(ctx, sdk, envelope.Name(), "property", "set", key, value)
}

func runFirmwareTool(ctx context.Context, sdk *SDK, envelopePath string, args ...string) error {
	args = append([]string{"-e", envelopePath}, args...)
	cmd := sdk.FirmwareTool(ctx, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func copySnapshotsIntoCache(ctx context.Context, sdk *SDK, envelope *os.File) error {
	// TODO(kasper): We should be able to get all the snapshots stored in
	// the envelope out through extraction. For now, we just make it work
	// with the jaguar snapshot.
	jaguarSnapshotPath, err := directory.GetJaguarSnapshotPath()
	if err != nil {
		return err
	}
	if err := copySnapshotIntoCache(jaguarSnapshotPath); err != nil {
		return err
	}

	snapshot, err := ExtractFirmwarePart(ctx, sdk, envelope.Name(), "system.snapshot")
	if err != nil {
		return err
	}
	defer snapshot.Close()

	if err := copySnapshotIntoCache(snapshot.Name()); err != nil {
		return err
	}
	return nil
}

func copySnapshotIntoCache(path string) error {
	uuid, err := GetUuid(path)
	if err != nil {
		return err
	}

	cacheDirectory, err := directory.GetSnapshotsCachePath()
	if err != nil {
		return err
	}

	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(filepath.Join(cacheDirectory, uuid.String()+".snapshot"))
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
