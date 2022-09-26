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

func getEsptoolURL(version string) string {
	currOS := runtime.GOOS
	if currOS == "darwin" {
		currOS = "macos"
	}
	return directory.Executable(fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/esptool-%s/esptool_%s_%s", version, currOS, version))
}

const (
	esptoolVersion = "v3.0"
)

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
				sdk, err := GetSDK(ctx)
				if err != nil {
					return err
				}

				if err := validateAssets(); err != nil {
					return err
				}

				if _, err := directory.GetJaguarSnapshotPath(); err != nil {
					return err
				}

				if _, err := directory.GetFirmwareEnvelopePath(); err != nil {
					return err
				}

				if _, err := directory.GetEsptoolPath(); err != nil {
					return err
				}

				if err := copySnapshotsIntoCache(ctx, sdk); err != nil {
					return err
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

			if err := downloadSDK(ctx, info.SDKVersion); err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			if err := downloadAssets(ctx, sdk, info.Version); err != nil {
				return err
			}

			if err := downloadEsptool(ctx, esptoolVersion); err != nil {
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

	cmd.Flags().BoolP("check", "c", false, "if set, will check the local setup")
	return cmd
}

func validateAssets() error {
	assetsPath, err := directory.GetAssetsPath()
	if err != nil {
		return err
	}
	paths := []string{
		filepath.Join(assetsPath, "firmware.envelope"),
	}
	for _, p := range paths {
		if err := checkFilepath(p, "invalid assets"); err != nil {
			return err
		}
	}
	return nil
}

func downloadAssets(ctx context.Context, sdk *SDK, version string) error {
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
		return fmt.Errorf("failed to read the Jaguar assets as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(assetsPath); err != nil {
		return err
	}

	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, assetsPath, "assets/"); err != nil {
		return fmt.Errorf("failed to extract the Jaguar assets, reason: %w", err)
	}
	gzipReader.Close()

	if err := copySnapshotsIntoCache(ctx, sdk); err != nil {
		return err
	}

	fmt.Println("Successfully installed Jaguar assets into", assetsPath)
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

func copySnapshotsIntoCache(ctx context.Context, sdk *SDK) error {
	jaguarSnapshotPath, err := directory.GetJaguarSnapshotPath()
	if err != nil {
		return err
	}
	if err := copySnapshotIntoCache(jaguarSnapshotPath); err != nil {
		return err
	}

	envelopePath, err := directory.GetFirmwareEnvelopePath()
	if err != nil {
		return err
	}

	systemSnapshot, err := ExtractFirmwarePart(ctx, sdk, envelopePath, "system.snapshot")
	if err != nil {
		return err
	}
	defer systemSnapshot.Close()

	if err := copySnapshotIntoCache(systemSnapshot.Name()); err != nil {
		return err
	}
	return nil
}

func downloadEsptool(ctx context.Context, version string) error {
	esptoolPath, err := directory.GetEsptoolCachePath()
	if err != nil {
		return err
	}

	esptoolURL := getEsptoolURL(version)
	fmt.Printf("Downloading esptool from %s ...\n", esptoolURL)
	bundle, err := download(ctx, esptoolURL)
	if err != nil {
		return err
	}
	defer bundle.Close()

	f, err := os.OpenFile(esptoolPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, bundle); err != nil {
		return err
	}
	bundle.Close()
	fmt.Println("Successfully installed esptool into", esptoolPath)
	return nil
}

func downloadSDK(ctx context.Context, version string) error {
	sdkPath, err := directory.GetSDKCachePath()
	if err != nil {
		return err
	}

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
		return nil, fmt.Errorf("failed downloading the Toit SDK artifact: %v", resp.Status)
	}

	progress := pb.New64(resp.ContentLength)
	return progress.Start().NewProxyReader(resp.Body), nil
}
