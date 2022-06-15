// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/setanta314/ar"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

// Checks whether a file is a snapshot file.  Starts by checking for an ar
// file, since snapshot files are ar files.
func IsSnapshot(filename string) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()
	magic_sequence := make([]byte, 8)
	_, err = io.ReadAtLeast(file, magic_sequence, 8)
	if err != nil {
		return false
	}
	if bytes.Compare(magic_sequence, []byte("!<arch>\n")) != 0 {
		return false
	}

	file.Seek(0, io.SeekStart)
	reader := ar.NewReader(file)
	header, err := reader.Next()
	if err != nil {
		return false
	}
	if header.Name != "toit" {
		return false
	}
	return true
}

// Get the UUID out of a snapshot file, which is an ar archive.
func GetUuid(filename string) (uuid.UUID, error) {
	source, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Failed to open '%s'n", filename)
		return uuid.Nil, err
	}
	reader := ar.NewReader(source)
	readAtLeastOneEntry := false
	for {
		header, err := reader.Next()
		if err != nil {
			if readAtLeastOneEntry {
				fmt.Printf("Did not include UUID: '%s'n", filename)
			} else {
				fmt.Printf("Not a snapshot file: '%s'n", filename)
			}
			return uuid.Nil, err
		}
		if header.Name == "uuid" {
			raw_uuid := make([]byte, 16)
			_, err = io.ReadAtLeast(reader, raw_uuid, 16)
			if err != nil {
				fmt.Printf("UUID in snapshot too short: '%s'n", filename)
				return uuid.Nil, err
			}
			return uuid.FromBytes(raw_uuid)
		}
	}
}

func RunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <file>",
		Short: "Run Toit code on a Jaguar device",
		Long: "Run the specified .toit file on a Jaguar device as a new program. If the\n" +
			"device is already executing another program, that program is stopped before\n" +
			"the new program is started.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetWorkspaceConfig()
			if err != nil {
				return err
			}

			entrypoint := args[0]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't run directory: '%s'", entrypoint)
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

			return RunFile(cmd, device, sdk, entrypoint)
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name or id")
	return cmd
}

func RunFile(cmd *cobra.Command, device *Device, sdk *SDK, path string) error {
	fmt.Printf("Running '%s' on '%s' ...\n", path, device.Name)
	ctx := cmd.Context()

	snapshotsCache, err := directory.GetSnapshotsCachePath()
	if err != nil {
		return err
	}

	var snapshot string = ""

	if IsSnapshot(path) {
		snapshot = path
	} else {
		// We are running a toit file, so we need to compile it to a
		// snapshot first.
		tempdir, err := ioutil.TempDir("", "jag_run")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempdir)

		snapshotFile, err := ioutil.TempFile(tempdir, "jag_run_*.snapshot")
		if err != nil {
			return err
		}
		snapshot = snapshotFile.Name()
		err = sdk.Compile(ctx, snapshot, path)
		if err != nil {
			// We assume the error has been printed.
			// Mark the command as silent to avoid printing the error twice.
			cmd.SilenceErrors = true
			return err
		}
	}

	programId, err := GetUuid(snapshot)
	if err != nil {
		return err
	}

	cacheDestination := filepath.Join(snapshotsCache, programId.String()+".snapshot")

	// Copy the snapshot into the cache dir so it is available for
	// decoding stack traces etc.  We want to add it to the cache in
	// an atomic rename, but atomic renames only work within a single
	// filesystem/mount point.  So we have to do this in two steps,
	// first copying to a temp file in the cache dir, then renaming
	// in that directory.
	if cacheDestination != snapshot {
		tempFileInCacheDirectory, err := ioutil.TempFile(snapshotsCache, "jag_run_*.snapshot")
		if err != nil {
			fmt.Printf("Failed to write temporary file in '%s'\n", snapshotsCache)
			return err
		}
		defer tempFileInCacheDirectory.Close()
		defer os.Remove(tempFileInCacheDirectory.Name())

		source, err := os.Open(snapshot)
		if err != nil {
			fmt.Printf("Failed to read '%s'n", snapshot)
			return err
		}
		defer source.Close()
		defer tempFileInCacheDirectory.Close()

		_, err = io.Copy(tempFileInCacheDirectory, source)
		if err != nil {
			fmt.Printf("Failed to write '%s'n", tempFileInCacheDirectory.Name())
			return err
		}
		tempFileInCacheDirectory.Close()

		// Atomic move so no other process can see a half-written snapshot file.
		err = os.Rename(tempFileInCacheDirectory.Name(), cacheDestination)
		if err != nil {
			return err
		}
	}

	b, err := sdk.Build(ctx, device, cacheDestination)
	if err != nil {
		// We assume the error has been printed.
		// Mark the command as silent to avoid printing the error twice.
		cmd.SilenceErrors = true
		return err
	}

	if err := device.Run(ctx, sdk, b); err != nil {
		fmt.Println("Error:", err)
		// We just printed the error.
		// Mark the command as silent to avoid printing the error twice.
		cmd.SilenceErrors = true
		return err
	}
	fmt.Printf("Success: Sent %dKB code to '%s'\n", len(b)/1024, device.Name)
	return nil
}
