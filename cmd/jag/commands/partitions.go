// Copyright (C) 2026 Toit contributors.
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

// getPartitionTableURL returns the URL of a partition table that is published
// in the Toit envelopes repository for the given SDK version.
func getPartitionTableURL(version string, name string) string {
	return fmt.Sprintf("https://github.com/toitlang/envelopes/releases/download/%s/partitions-%s.csv", version, name)
}

// partitionOverrideArgs returns the firmware-tool arguments that override the
// partition table, together with a cleanup function that must be invoked once
// the arguments are no longer needed.
//
// If the user explicitly requested a partition table (via '--partition-table'),
// it is resolved just like an envelope: a path or URL is used as-is, and a bare
// name is fetched from the Toit envelopes repository. Otherwise no override is
// applied and the partition table shipped in the envelope is used.
func partitionOverrideArgs(ctx context.Context, partitionTable string, jagVersion string, sdkVersion string) (args []string, cleanup func(), err error) {
	noop := func() {}

	if partitionTable == "" {
		return nil, noop, nil
	}

	path, cleanup, err := resolvePartitionTable(ctx, partitionTable, jagVersion, sdkVersion)
	if err != nil {
		return nil, noop, err
	}
	return []string{"--partitions", path}, cleanup, nil
}

// resolvePartitionTable turns the value of the '--partition-table' flag into a
// path to a local CSV file. It mirrors the way envelopes are resolved:
//   - an existing file is used directly,
//   - a URL is downloaded to a temporary file,
//   - a bare name (no '/' or '.') is fetched from the Toit envelopes repository
//     for the given SDK version and cached,
//   - anything else is returned as-is and will fail later with a clear error.
//
// The returned cleanup function removes any temporary file that was created and
// must always be called by the caller.
func resolvePartitionTable(ctx context.Context, value string, jagVersion string, sdkVersion string) (path string, cleanup func(), err error) {
	noop := func() {}

	// An existing file is used directly.
	if _, err := os.Stat(value); err == nil {
		return value, noop, nil
	}

	// A URL is downloaded to a temporary file.
	if isURL(value) {
		fmt.Printf("Downloading partition table from %s ...\n", value)
		file, err := downloadPartitionTableToTemp(ctx, value)
		if err != nil {
			return "", noop, err
		}
		fmt.Printf("Successfully downloaded partition table\n")
		return file, func() { os.Remove(file) }, nil
	}

	// A bare name is fetched from the published partition tables.
	if !strings.ContainsAny(value, "/.") {
		path, err := getCachedPartitionTablePath(ctx, jagVersion, sdkVersion, value)
		if err != nil {
			return "", noop, err
		}
		return path, noop, nil
	}

	// Return the original path. This will yield a "failed to open" error later.
	return value, noop, nil
}

func downloadPartitionTableToTemp(ctx context.Context, url string) (string, error) {
	bundle, err := download(ctx, url)
	if err != nil {
		return "", err
	}
	defer bundle.Close()

	file, err := os.CreateTemp("", "*.partitions.csv")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, bundle); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// getCachedPartitionTablePath returns the path to the cached partition table for
// the given name, downloading it from the Toit envelopes repository if needed.
func getCachedPartitionTablePath(ctx context.Context, jagVersion string, sdkVersion string, name string) (string, error) {
	tablesDir, err := directory.GetPartitionTablesCachePath(jagVersion)
	if err != nil {
		return "", err
	}

	path := filepath.Join(tablesDir, sdkVersion, fmt.Sprintf("partitions-%s.csv", name))
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	url := getPartitionTableURL(sdkVersion, name)
	fmt.Printf("Downloading partition table '%s' from %s ...\n", name, url)

	tmpPath, err := downloadPartitionTableToTemp(ctx, url)
	if err != nil {
		fmt.Printf("Failed to download partition table: %v\n", err)
		fmt.Printf("Make sure the name is correct. You can find supported partition tables at\n")
		fmt.Printf("https://github.com/toitlang/envelopes/tree/main/partitions/esp32.\n")
		fmt.Printf("The name of the partition is then 'esp32-' plus the directory name.\n")
		fmt.Printf("Example: 'esp32-ota-1c0000-16mb'.\n")
		return "", err
	}
	defer os.Remove(tmpPath)

	if err := copyFile(tmpPath, path); err != nil {
		return "", err
	}

	fmt.Printf("Successfully installed partition table '%s'\n", name)
	return path, nil
}

func copyFile(src string, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
