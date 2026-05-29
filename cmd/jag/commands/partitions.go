// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"embed"
	"os"
)

//go:embed partitions/*.csv
var partitionTables embed.FS

// chipsWithPartitionOverride maps a chip type to the embedded partition table
// that Jaguar should use instead of the one shipped in the envelope.
//
// This is a workaround for SDK v2.0.0-alpha.194, where the firmware image for
// the ESP32-C6 grew past the 0x1b0000 OTA partitions of the envelope's default
// table. See partitions/esp32c6.csv for details. Remove the affected entries
// once the envelopes/SDK ship large enough OTA partitions.
var chipsWithPartitionOverride = map[string]string{
	"esp32c6": "partitions/esp32c6.csv",
}

// partitionOverrideArgs returns the firmware-tool arguments that override the
// partition table for the given chip, or nil if no override is needed. When a
// non-nil slice is returned, the caller must invoke cleanup once the arguments
// are no longer needed to remove the temporary file.
func partitionOverrideArgs(chip string) (args []string, cleanup func(), err error) {
	noop := func() {}

	asset, ok := chipsWithPartitionOverride[chip]
	if !ok {
		return nil, noop, nil
	}

	contents, err := partitionTables.ReadFile(asset)
	if err != nil {
		return nil, noop, err
	}

	file, err := os.CreateTemp("", "*.partitions.csv")
	if err != nil {
		return nil, noop, err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, noop, err
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return nil, noop, err
	}

	cleanup = func() { os.Remove(file.Name()) }
	return []string{"--partitions", file.Name()}, cleanup, nil
}
