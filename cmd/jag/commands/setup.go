// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
)

func getToitSDKURL(version string) string {
	currOS := runtime.GOOS
	if currOS == "darwin" {
		currOS = "macos"
	}
	return fmt.Sprintf("https://github.com/toitlang/toit/releases/download/%s/toit-%s.tar.gz", version, currOS)
}

func getSnapshotURL(version string) string {
	return fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/sdk-%s/jaguar.snapshot", version)
}

func getESP32ImageURL(version string) string {
	return fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/sdk-%s/image.tar.gz", version)
}

func getEsptoolURL(version string) string {
	currOS := runtime.GOOS
	if currOS == "darwin" {
		currOS = "macos"
	}
	return executable(fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/sdk-%s/esptool_%s_v3.0", version, currOS))
}

func SetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup the Toit SDK",
		Long: "Setup the Toit SDK by downloading the necessary bits from https://github.com/toitlang/toit.\n" +
			"The downloaded SDK is stored locally in a subdirectory of your home folder.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := cmd.Flags().GetString("version")
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if err := downloadSDK(ctx, version); err != nil {
				return err
			}

			if err := downloadESP32Image(ctx, version); err != nil {
				return err
			}

			if err := downloadSnapshot(ctx, version); err != nil {
				return err
			}

			if err := downloadEsptool(ctx, version); err != nil {
				return err
			}

			fmt.Printf("Successfully setup Jaguar with Toit SDK %s.\n", version)

			return nil
		},
	}

	cmd.Flags().StringP("version", "v", "v0.0.4", "Toit SDK version to download")
	return cmd
}

func downloadESP32Image(ctx context.Context, version string) error {
	imagePath, err := GetESP32ImageCachePath()
	if err != nil {
		return err
	}

	esp32ImageURL := getESP32ImageURL(version)
	fmt.Printf("Downloading ESP32 image from %s...\n", esp32ImageURL)
	bundle, err := download(ctx, esp32ImageURL)
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(bundle)
	if err != nil {
		bundle.Close()
		return fmt.Errorf("failed to read the ESP32 image artifact as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(imagePath); err != nil {
		return err
	}

	if err := os.MkdirAll(imagePath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, imagePath, "image/"); err != nil {
		return fmt.Errorf("failed to extract the ESP32 image, reason: %w", err)
	}
	gzipReader.Close()
	fmt.Println("Successfully installed ESP32 image into", imagePath)
	return nil
}

func downloadSnapshot(ctx context.Context, version string) error {
	snapshotPath, err := GetSnapshotCachePath()
	if err != nil {
		return err
	}

	snapshotURL := getSnapshotURL(version)
	fmt.Printf("Downloading snapshot from %s...\n", snapshotURL)
	bundle, err := download(ctx, snapshotURL)
	if err != nil {
		return err
	}
	defer bundle.Close()

	f, err := os.OpenFile(snapshotPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, bundle); err != nil {
		return err
	}
	bundle.Close()
	fmt.Println("Successfully installed snapshot into", snapshotPath)
	return nil
}

func downloadEsptool(ctx context.Context, version string) error {
	esptoolPath, err := GetEsptoolCachePath()
	if err != nil {
		return err
	}

	esptoolURL := getEsptoolURL(version)
	fmt.Printf("Downloading esptool from %s...\n", esptoolURL)
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
	sdkPath, err := GetSDKCachePath()
	if err != nil {
		return err
	}

	sdkURL := getToitSDKURL(version)
	fmt.Printf("Downloading Toit SDK from %s...\n", sdkURL)
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
