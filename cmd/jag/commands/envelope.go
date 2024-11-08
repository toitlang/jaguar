// Copyright (C) 2023 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

// GetCachedFirmwareEnvelopePath returns the path to the cached firmware envelope.
// If necessary, downloads the envelope from the server first.
func GetCachedFirmwareEnvelopePath(ctx context.Context, version string, model string) (string, error) {
	// Envelopes published in the Toit envelope repository are always lowercase.
	model = strings.ToLower(model)
	path, err := getFirmwareEnvelopePath(version, model)
	if err != nil && err != os.ErrNotExist {
		return "", err
	}
	if err == os.ErrNotExist {
		// Download the envelope from the server.
		if err := downloadPublishedFirmware(ctx, version, model); err != nil {
			fmt.Printf("Failed to download firmware: %v\n", err)
			switch model {
			case "esp32", "esp32s3", "esp32c3", "esp32s2":
				// These names are correct. Simply return the error.
			case "ESP32-S3", "ESP32-C3", "ESP32-S2":
				fmt.Printf("Chip model names must be lowercase without dashes. Please try again with 'esp32s3', 'esp32c3', or 'esp32s2'.\n")
			default:
				fmt.Printf("Make sure the model name is correct. You can find supported models at\n")
				fmt.Printf("https://github.com/toitlang/envelopes/releases/tag/%s.\n", version)
			}
			return "", err
		}
	}
	return path, nil
}

func getFirmwareURL(version string, model string) string {
	return fmt.Sprintf("https://github.com/toitlang/envelopes/releases/download/%s/firmware-%s.envelope.gz", version, model)
}

func isURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

func downloadGzipped(ctx context.Context, url string, path string) error {
	bundle, err := download(ctx, url)
	if err != nil {
		return err
	}
	defer bundle.Close()
	return storeGzipped(bundle, path)
}

func storeGzipped(bundle io.ReadCloser, path string) error {
	// If the path is a zip file, unzip it into the tmpDir.
	gzipReader, err := newGZipReader(bundle)
	if err != nil {
		return fmt.Errorf("failed to read firmware as gzip file: %w", err)
	}
	defer gzipReader.Close()

	// Create a file in the tmpDir to store the envelope.
	destination, err := os.Create(path)
	if err != nil {
		return err
	}
	defer destination.Close()

	// Copy the envelope to the file.
	_, err = io.Copy(destination, gzipReader)
	if err != nil {
		return err
	}

	return nil
}

func DownloadEnvelope(ctx context.Context, path string, version string, tmpDir string) (string, error) {
	// Check if the envelopes file exists. If yes, then we already have the envelope.
	if _, err := os.Stat(path); err == nil {
		fileReader, err := os.Open(path)
		if err != nil {
			return "", err
		}

		unzippedPath := filepath.Join(tmpDir, "firmware.envelope")
		err = storeGzipped(fileReader, unzippedPath)
		if err != nil {
			// Assume it is not a gzip file and return the path.
			return path, nil
		}
		return unzippedPath, nil
	}

	// If the path is a URL, download the envelope from there and store it in the tmpDir.
	if isURL(path) {
		fmt.Printf("Downloading firmware from %s ...\n", path)
		bundle, err := download(ctx, path)
		if err != nil {
			return "", err
		}

		unzippedPath := filepath.Join(tmpDir, "firmware.envelope")
		err = storeGzipped(bundle, unzippedPath)
		if err != nil {
			return "", err
		}

		fmt.Printf("Successfully downloaded firmware\n")
		return unzippedPath, nil
	}

	if !strings.ContainsAny(path, "/.") {
		// Try to read it as if it was a published envelope.
		return GetCachedFirmwareEnvelopePath(ctx, version, path)
	}
	// Return the original path. This will yield an "Failed to open" error later.
	return path, nil
}

func downloadPublishedFirmware(ctx context.Context, version string, model string) error {
	envelopesDir, err := directory.GetEnvelopesCachePath(version)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(envelopesDir, 0755); err != nil {
		return err
	}

	firmwareURL := getFirmwareURL(version, model)
	fmt.Printf("Downloading %s firmware from %s ...\n", model, firmwareURL)

	envelopeFileName := GetFirmwareEnvelopeFileName(model)
	envelopePath := filepath.Join(envelopesDir, envelopeFileName)
	// First download to a temporary file, in case the download doesn't complete.
	tmpEnvelopePath := envelopePath + ".tmp"
	err = downloadGzipped(ctx, firmwareURL, tmpEnvelopePath)
	if err != nil {
		return err
	}

	// Rename the temporary file to the final file.
	err = os.Rename(tmpEnvelopePath, envelopePath)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully installed %s firmware into %s\n", model, envelopesDir)
	return nil
}

func GetFirmwareEnvelopeFileName(model string) string {
	return fmt.Sprintf("firmware-%s.envelope", model)
}

// getFirmwareEnvelopePath returns the firmware envelope path for the given model.
// If the file doesn't exist returns the correct path but sets err to `os.ErrNotExist`.
func getFirmwareEnvelopePath(version string, model string) (string, error) {
	repoPath, ok := directory.GetRepoPath()
	if ok {
		return filepath.Join(repoPath, "build", model, "firmware.envelope"), nil
	}

	envelopesPath, err := directory.GetEnvelopesCachePath(version)
	if err != nil {
		return "", nil
	}

	name := GetFirmwareEnvelopeFileName(model)
	path := filepath.Join(envelopesPath, name)
	if stat, err := os.Stat(path); err != nil || stat.IsDir() {
		if stat != nil && stat.IsDir() {
			return "", fmt.Errorf("the path '%s' holds a directory, not the firmware envelope for '%s'", envelopesPath, model)
		}
		// File doesn't exist.
		return path, os.ErrNotExist
	}
	return path, nil
}
