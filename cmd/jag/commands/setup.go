// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func getToitSDKURL(version string) string {
	currOS := runtime.GOOS
	if currOS == "darwin" {
		currOS = "macos"
	}
	return fmt.Sprintf("https://github.com/toitlang/toit/releases/download/%s/toit-%s.tar.gz", version, currOS)
}

func getAssetsURL(version string) string {
	return fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/%s/assets.tar.gz", version)
}

func getFirmwareURL(version string, model string) string {
	return fmt.Sprintf("https://github.com/toitlang/toit/releases/download/%s/firmware-%s.gz", version, model)
}

func SetupCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup the Toit SDK",
		Long: "Setup the Toit SDK by downloading the necessary bits from https://github.com/toitlang/toit.\n" +
			"The downloaded SDK is stored locally in a subdirectory of your home folder.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			check, err := cmd.Flags().GetBool("check")
			if err != nil {
				return err
			}

			if check {
				if _, err := GetSDK(ctx); err != nil {
					return err
				}

				if _, err := directory.GetJaguarSnapshotPath(); err != nil {
					return err
				}

				models := directory.GetFirmwareModels()
				for _, model := range models {
					if _, err := directory.GetFirmwareEnvelopePath(model); err != nil {
						return err
					}
				}

				fmt.Println("Jaguar setup is valid.")
				return nil
			}

			sdkPath, err := directory.GetSDKCachePath()
			if err != nil {
				return err
			}
			downloaderPath := filepath.Join(sdkPath, "JAGUAR")
			os.Remove(downloaderPath)

			if err := downloadSdk(ctx, info.SDKVersion); err != nil {
				return err
			}

			if err := downloadAssets(ctx, info.Version); err != nil {
				return err
			}

			if err := downloadFirmwareAll(ctx, info.SDKVersion); err != nil {
				return err
			}

			downloaderBytes, err := json.Marshal(&info)
			if err != nil {
				return err
			}

			if err := os.WriteFile(downloaderPath, downloaderBytes, 0666); err != nil {
				return err
			}

			fmt.Printf("Successfully setup Jaguar %s with Toit SDK %s.\n", info.Version, info.SDKVersion)

			return nil
		},
	}
	cmd.AddCommand(SetupSdkCmd(info))
	cmd.Flags().BoolP("check", "c", false, "if set, will check the local setup")
	return cmd
}

func SetupSdkCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sdk",
		Short:        "Setup just the SDK",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) != 1 {
				return fmt.Errorf("takes exactly one argument")
			}
			if err := downloadSdkTo(ctx, info.SDKVersion, args[0]); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func downloadAssets(ctx context.Context, version string) error {
	assetsPath, err := directory.GetAssetsCachePath()
	if err != nil {
		return err
	}

	assetsURL := getAssetsURL(version)
	fmt.Printf("Downloading Jaguar assets from %s ...\n", assetsURL)
	bundle, err := download(ctx, assetsURL)
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(bundle)
	if err != nil {
		bundle.Close()
		return fmt.Errorf("failed to read Jaguar assets as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(assetsPath); err != nil {
		return err
	}

	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, assetsPath, "assets/"); err != nil {
		return fmt.Errorf("failed to extract Jaguar assets, reason: %w", err)
	}

	fmt.Println("Successfully installed Jaguar assets into", assetsPath)
	return nil
}

func downloadFirmwareAll(ctx context.Context, version string) error {
	models := directory.GetFirmwareModels()
	for _, model := range models {
		if err := downloadFirmware(ctx, version, model); err != nil {
			return err
		}
	}
	return nil
}

func downloadFirmware(ctx context.Context, version string, model string) error {
	assetsPath, err := directory.GetAssetsCachePath()
	if err != nil {
		return err
	}

	firmwareURL := getFirmwareURL(version, model)
	fmt.Printf("Downloading %s firmware from %s ...\n", model, firmwareURL)
	bundle, err := download(ctx, firmwareURL)
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(bundle)
	if err != nil {
		bundle.Close()
		return fmt.Errorf("failed to read %s firmware as gzip file: %w", model, err)
	}
	defer gzipReader.Close()

	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		return err
	}

	envelopeFileName := directory.GetFirmwareEnvelopeFileName(model)
	destination, err := os.Create(filepath.Join(assetsPath, envelopeFileName))
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, gzipReader)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully installed %s firmware into %s\n", model, assetsPath)
	return nil
}

func downloadSdk(ctx context.Context, version string) error {
	sdkPath, err := directory.GetSDKCachePath()
	if err != nil {
		return err
	}
	return downloadSdkTo(ctx, version, sdkPath)
}

func downloadSdkTo(ctx context.Context, version string, sdkPath string) error {
	sdkURL := getToitSDKURL(version)
	fmt.Printf("Downloading Toit SDK from %s ...\n", sdkURL)
	sdk, err := download(ctx, getToitSDKURL(version))
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(sdk)
	if err != nil {
		sdk.Close()
		return fmt.Errorf("failed to read the Toit SDK artifact as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(sdkPath); err != nil {
		return err
	}

	if err := os.MkdirAll(sdkPath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, sdkPath, "toit/"); err != nil {
		return fmt.Errorf("failed to extract the Toit SDK, reason: %w", err)
	}
	gzipReader.Close()
	fmt.Println("Successfully installed Toit SDK into", sdkPath)
	return nil
}

func download(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed downloading from %s: %v", url, resp.Status)
	}

	progress := pb.New64(resp.ContentLength)
	return progress.Start().NewProxyReader(resp.Body), nil
}
