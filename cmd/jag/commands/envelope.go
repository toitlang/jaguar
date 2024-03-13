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
	path, err := getFirmwareEnvelopePath(version, model)
	if err != nil && err != os.ErrNotExist {
		return "", err
	}
	if err == os.ErrNotExist {
		// Download the envelope from the server.
		if err := downloadPublishedFirmware(ctx, version, model); err != nil {
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

func storeGzippedInDirDir(fileReader io.ReadCloser, dir string) (string, error) {
	// If the path is a zip file, unzip it into the tmpDir.
	gzipReader, err := newGZipReader(fileReader)
	if err != nil {
		// Assume it is not a gzip file and return the path.
		return "", err
	}
	defer gzipReader.Close()

	// Create a file in the tmpDir to store the envelope.
	destination, err := os.Create(filepath.Join(dir, "firmware.envelope"))
	if err != nil {
		return "", err
	}
	defer destination.Close()

	// Copy the envelope to the file.
	_, err = io.Copy(destination, gzipReader)
	if err != nil {
		return "", err
	}

	return destination.Name(), nil
}

func DownloadEnvelope(ctx context.Context, path string, version string, tmpDir string) (string, error) {
	// Check if the envelopes file exists. If yes, then we already have the envelope.
	if _, err := os.Stat(path); err == nil {
		fileReader, err := os.Open(path)
		if err != nil {
			return "", err
		}

		result, err := storeGzippedInDirDir(fileReader, tmpDir)
		if err != nil {
			// Assume it is not a gzip file and return the path.
			return path, nil
		}
		return result, nil
	}

	// If the path is a URL, download the envelope from there and store it in the tmpDir.
	if isURL(path) {
		fmt.Printf("Downloading firmware from %s ...\n", path)
		bundle, err := download(ctx, path)
		if err != nil {
			return "", err
		}

		return storeGzippedInDirDir(bundle, tmpDir)
	}

	// Try to read it as if it was a published envelope.
	return GetCachedFirmwareEnvelopePath(ctx, version, path)
}

func downloadPublishedFirmware(ctx context.Context, version string, model string) error {
	envelopesPath, err := directory.GetEnvelopesCachePath(version)
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

	if err := os.MkdirAll(envelopesPath, 0755); err != nil {
		return err
	}

	envelopeFileName := GetFirmwareEnvelopeFileName(model)
	destination, err := os.Create(filepath.Join(envelopesPath, envelopeFileName))
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, gzipReader)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully installed %s firmware into %s\n", model, envelopesPath)
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
