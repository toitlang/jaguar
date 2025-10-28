// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func FlashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flash [envelope]",
		Short: "Flash an ESP32 with the Jaguar firmware",
		Long: "Flash an ESP32 with the Jaguar firmware. The initial flashing is\n" +
			"done over a serial connection and it is used to give the ESP32 its initial\n" +
			"firmware and the necessary WiFi credentials.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			port, err := cmd.Flags().GetString("port")
			if err != nil {
				return err
			}
			shouldSkipPortCheck, err := cmd.Flags().GetBool("skip-port-check")
			if err != nil {
				return err
			}
			if !shouldSkipPortCheck {
				if port, err = CheckPort(port); err != nil {
					return err
				}
			}

			baud, err := cmd.Flags().GetUint("baud")
			if err != nil {
				return err
			}

			probeChipType := func(ctx context.Context, sdk *SDK) (string, error) {
				result, err := ProbeChipType(ctx, port, sdk)
				if err == nil {
					if exists, err := PortExists(port); err != nil || !exists {
						// Some boards leave flash mode and require manual intervention after
						// ever interaction. Tell the user how to work around this.
						fmt.Println("Note: Your board disappeared after probing the chip type.\n" +
							"This can happen on some boards that require manual intervention to enter flash mode.\n" +
							"Use '--chip=" + result + "' to avoid this probe step in the future.")
					}
				}
				return result, err
			}
			return withFirmware(cmd, args, probeChipType, nil, func(id string, envelopeFile *os.File, config map[string]interface{}) error {

				sdk, err := GetSDK(ctx)
				if err != nil {
					return err
				}

				flashArguments := []string{
					"flash",
					"--port", port,
					"--baud", strconv.Itoa(int(baud)),
				}

				// Golang equivalent of #ifdef Windows.  We skip this
				// because the whole uucp group issue does not affect
				// Windows, but on the other hand Windows has strange
				// escaping rules for COM ports over 10 (COM10, COM11),
				// which we don't want to deal with.
				if os.PathSeparator != '\\' && !shouldSkipPortCheck {
					// Use golang to check the port can be opened for writing first.
					// This is to avoid the error message from esptool.py, which is
					// confusing to users in the common case where the port is owned
					// by the dialout or uucp group.
					file, err := os.OpenFile(port, os.O_WRONLY, 0)
					if err != nil {
						return err
					}
					// Close the file again:
					file.Close()
				}

				fmt.Printf("Flashing device over serial on port '%s' ...\n", port)
				return runFirmwareToolWithConfig(ctx, sdk, envelopeFile.Name(), config, flashArguments...)
			})
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "serial port to flash via")
	cmd.Flags().Uint("baud", 921600, "baud rate used for the serial flashing")
	cmd.Flags().Bool("skip-port-check", false, "accept the given port without checking")
	addFirmwareFlashFlags(cmd, "name for the device, if not set a name will be auto generated")
	return cmd
}

func ProbeChipType(ctx context.Context, port string, sdk *SDK) (string, error) {
	// Get the esptool from the SDK.
	cmd := sdk.EspTool(ctx, "--port", port, "chip-id")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to probe chip type: %w: %s", err, string(output))
	}
	// Parse the output to find the chip type.
	// There should be a string "Connected to ESP32 on ..."
	outputStr := string(output)
	if len(outputStr) == 0 {
		return "", fmt.Errorf("failed to probe chip type: empty output")
	}
	// Find the "Connected to ".
	prefix := "Connected to "
	start := strings.Index(outputStr, prefix)
	if start >= 0 {
		// Find the " on ".
		start += len(prefix)
		end := strings.Index(outputStr[start:], " on ")
		if end >= 0 {
			// Extract the chip type.
			chip := outputStr[start : start+end]
			// Lower case, and remove '-'.
			chip = strings.ToLower(strings.ReplaceAll(chip, "-", ""))
			return chip, nil
		}
	}

	return "", fmt.Errorf("failed to probe chip type: unexpected output: %s", outputStr)
}
