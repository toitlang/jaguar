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
		if err := downloadFirmware(ctx, version, model); err != nil {
			return "", err
		}
	}
	return path, nil
}

func getFirmwareURL(version string, model string) string {
	return fmt.Sprintf("https://github.com/toitlang/envelopes/releases/download/%s/firmware-%s.envelope.gz", version, model)
}

func downloadFirmware(ctx context.Context, version string, model string) error {
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
