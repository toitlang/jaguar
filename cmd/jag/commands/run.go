// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	if !bytes.Equal(magic_sequence, []byte("!<arch>\n")) {
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
			"the new program is started.\n" +
			"If you specify the device to be 'host' with the option '-d host', then the\n" +
			"program runs on the current computer instead.\n" +
			"\n" +
			"The following define flags have a special meaning:\n" +
			"	'-D jag.wifi=false': Disable Jaguar's WiFi-based HTTP server while the program.\n" +
			"     is running.\n" +
			"	'-D jag.timeout': Set the timeout for Jaguar to wait for the program to\n" +
			"     finish. The value can be a number of seconds or a duration string.\n" +
			"     If jag.wifi=false is set, then the default is 10 seconds.\n" +
			"\n" +
			"For example 'jag run -D jag.wifi=false wifi-scan.toit' will run the wifi-scan\n" +
			"program on the device without Jaguar using the network.",
		Args:         cobra.MinimumNArgs(0),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			optimizationLevel := -1
			if cmd.Flags().Changed("optimization-level") {
				optimizationLevel, err = cmd.Flags().GetInt("optimization-level")
				if err != nil {
					return err
				}
			}

			if name, ok := deviceSelect.(deviceNameSelect); ok && string(name) == "host" {
				if cmd.Flags().Changed("define") {
					return fmt.Errorf("--define/-D is not yet supported when running on host")
				}
				return runOnHost(ctx, cmd, args, optimizationLevel)
			}

			if cmd.Flags().Changed("expression") {
				return fmt.Errorf("--expression/-s is not yet supported when running on devices")
			}

			if len(args) == 0 {
				return fmt.Errorf("no input file provided")
			} else if len(args) > 1 {
				return fmt.Errorf("passing arguments is only supported with 'jag run -d host'")
			}

			programAssetsPath, err := GetProgramAssetsPath(cmd.Flags(), "assets")
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

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			defines, err := parseDefineFlags(cmd, "define")
			if err != nil {
				return err
			}

			return RunFile(cmd, device, sdk, entrypoint, defines, programAssetsPath, optimizationLevel)
		},
	}

	cmd.Flags().StringP("expression", "s", "", "evaluate immediate Toit expression")
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	cmd.Flags().StringArrayP("define", "D", nil, "define settings to control run on device")
	cmd.Flags().String("assets", "", "attach assets to the program")
	cmd.Flags().IntP("optimization-level", "O", 1, "optimization level")
	return cmd
}

func runOnHost(ctx context.Context, cmd *cobra.Command, args []string, optimizationLevel int) error {
	sdk, err := GetSDK(ctx)
	if err != nil {
		return err
	}

	if optimizationLevel >= 0 {
		args = append([]string{"-O" + strconv.Itoa(optimizationLevel)}, args...)
	}

	expression, err := cmd.Flags().GetString("expression")
	if err != nil {
		return err
	}

	var runCmd *exec.Cmd

	if expression != "" {
		expressionArgs := append([]string{"-s", expression}, args...)
		runCmd = sdk.ToitRun(ctx, expressionArgs...)
	} else {
		runCmd = sdk.ToitRun(ctx, args...)
	}

	runCmd.Stderr = os.Stderr
	runCmd.Stdout = os.Stdout
	runCmd.Stdin = os.Stdin
	return runCmd.Run()
}

func RunFile(
	cmd *cobra.Command,
	device Device,
	sdk *SDK,
	path string,
	defines map[string]interface{},
	assetsPath string,
	optimizationLevel int) error {
	fmt.Printf("Running '%s' on '%s' ...\n", path, device.Name())
	return sendCodeFromFile(cmd, device, sdk, "/run", path, "", defines, assetsPath, optimizationLevel)
}

func InstallFile(
	cmd *cobra.Command,
	device Device,
	sdk *SDK,
	name string,
	path string,
	defines map[string]interface{},
	assetsPath string,
	optimizationLevel int) error {
	fmt.Printf("Installing container '%s' from '%s' on '%s' ...\n", name, path, device.Name())
	return sendCodeFromFile(cmd, device, sdk, "/install", path, name, defines, assetsPath, optimizationLevel)
}

func sendCodeFromFile(
	cmd *cobra.Command,
	device Device,
	sdk *SDK,
	request string,
	path string,
	name string,
	defines map[string]interface{},
	assetsPath string,
	optimizationLevel int) error {

	ctx := cmd.Context()
	snapshotsStateDir, err := directory.GetSnapshotsStatePath()
	if err != nil {
		return err
	}

	var snapshot string = ""

	if IsSnapshot(path) {
		snapshot = path
	} else {
		// We are running a toit file, so we need to compile it to a
		// snapshot first.
		tempdir, err := os.MkdirTemp("", "jag_run")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempdir)

		snapshotFile, err := os.CreateTemp(tempdir, "jag_run_*.snapshot")
		if err != nil {
			return err
		}
		snapshot = snapshotFile.Name()
		err = sdk.Compile(ctx, snapshot, path, optimizationLevel)
		if err != nil {
			// We assume the error has been printed.
			// Mark the command as silent to avoid printing the error twice.
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			return err
		}
	}

	programId, err := GetUuid(snapshot)
	if err != nil {
		return err
	}

	cacheDestination := filepath.Join(snapshotsStateDir, programId.String()+".snapshot")

	// Copy the snapshot into the cache dir so it is available for
	// decoding stack traces etc.  We want to add it to the cache in
	// an atomic rename, but atomic renames only work within a single
	// filesystem/mount point.  So we have to do this in two steps,
	// first copying to a temp file in the cache dir, then renaming
	// in that directory.
	if cacheDestination != snapshot {
		tempFileInCacheDirectory, err := os.CreateTemp(snapshotsStateDir, "jag_run_*.snapshot")
		if err != nil {
			fmt.Printf("Failed to write temporary file in '%s'\n", snapshotsStateDir)
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

	// Split the -D options into the ones we pass in the HTTP header for Jaguar
	// and the ones we send along as assets.
	headersMap := make(map[string]string)
	headersMap[JaguarContainerNameHeader] = name
	assetsMap := make(map[string]interface{})
	for key, value := range defines {
		if strings.HasPrefix(key, "jag.") {
			if key == "jag.disabled" || key == "jag.wifi" {
				if key == "jag.disabled" {
					fmt.Println("Warning: jag.disabled is deprecated, use jag.wifi=false instead")
				}
				switch converted := value.(type) {
				case bool:
					if !converted {
						headersMap[JaguarWifiDisabledHeader] = "true"
					}
				default:
					return fmt.Errorf("jag.wifi must be a bool")
				}
			} else if key == "jag.timeout" {
				switch converted := value.(type) {
				case int:
					headersMap[JaguarContainerTimeoutHeader] = fmt.Sprint(converted)
				case string:
					duration, err := time.ParseDuration(converted)
					if err != nil {
						return fmt.Errorf("cannot parse jag.timeout ('%s') as a duration", converted)
					}
					headersMap[JaguarContainerTimeoutHeader] = fmt.Sprint(int(math.Ceil(duration.Seconds())))
				default:
					return fmt.Errorf("jag.timeout must be a string or an int")
				}
			} else if key == "jag.interval" {
				switch converted := value.(type) {
				case string:
					_, err := time.ParseDuration(converted)
					if err != nil {
						return fmt.Errorf("cannot parse jag.interval ('%s') as a duration", converted)
					}
					headersMap[JaguarContainerIntervalHeader] = converted
				default:
					return fmt.Errorf("cannot parse jag.interval ('%s') as a duration", converted)
				}
			} else {
				return fmt.Errorf("unsupported Jaguar define: %s", key)
			}
		} else {
			assetsMap[key] = value
		}
	}

	if len(assetsMap) > 0 {
		temporaryAssetsFile, err := os.CreateTemp("", "jag_run_*.assets")
		if err != nil {
			return err
		}
		defer temporaryAssetsFile.Close()
		defer os.Remove(temporaryAssetsFile.Name())
		buildAssets(ctx, sdk, temporaryAssetsFile, assetsPath, assetsMap)
		assetsPath = temporaryAssetsFile.Name()
	}

	b, err := sdk.Build(ctx, device, cacheDestination, assetsPath)
	if err != nil {
		// We assume the error has been printed.
		// Mark the command as silent to avoid printing the error twice.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return err
	}
	startSend := time.Now()
	if err := device.SendCode(ctx, sdk, request, b, headersMap); err != nil {
		fmt.Println("Error:", err)
		// We just printed the error.
		// Mark the command as silent to avoid printing the error twice.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return err
	}
	elapsed := time.Since(startSend)
	fmt.Printf("Success: Sent %dKB code to '%s' in %.2fs\n", len(b)/1024, device.Name(), elapsed.Seconds())
	return nil
}

func buildAssets(ctx context.Context, sdk *SDK, output *os.File, inputPath string, assetsMap map[string]interface{}) error {
	// Write the defines into a temporary file as JSON.
	definesJsonFile, err := os.CreateTemp("", "jag_run_*.defines")
	if err != nil {
		return err
	}
	definesJson, err := json.Marshal(assetsMap)
	if err != nil {
		return err
	}
	os.WriteFile(definesJsonFile.Name(), definesJson, 0777)
	defer definesJsonFile.Close()
	defer os.Remove(definesJsonFile.Name())

	// Create a new assets file or copy the existing one.
	if inputPath == "" {
		if err := runAssetsTool(ctx, sdk, output.Name(), "create"); err != nil {
			return err
		}
		inputPath = output.Name()
	}

	// Add the defines as a TISON asset under with the name jag.defines.
	return runAssetsTool(ctx, sdk, inputPath, "add", "-o", output.Name(), "--format=tison", "jag.defines", definesJsonFile.Name())
}

func runAssetsTool(ctx context.Context, sdk *SDK, assetsPath string, args ...string) error {
	args = append([]string{"-e", assetsPath}, args...)
	cmd := sdk.AssetsTool(ctx, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
