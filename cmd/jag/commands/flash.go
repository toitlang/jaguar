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
				if err != nil {
					return result, err
				}
				// Probing the chip type talks to the chip over serial, which resets
				// it. Boards that use the ESP32's integrated USB peripheral (for
				// example the ESP32-S2) are taken out of their manually-entered
				// download mode by this, and their serial port disappears. There is
				// no reliable way to probe without disturbing such boards, so detect
				// the situation and tell the user how to skip the probe. We already
				// have the chip type from the probe, so we can name the exact flag.
				if exists, existsErr := PortExists(port); existsErr == nil && !exists {
					return "", fmt.Errorf(
						"the board left download mode after probing its chip type.\n"+
							"This happens on boards that use the ESP32's integrated USB peripheral (for example\n"+
							"the ESP32-S2) and must be put into download mode manually. Re-enter download mode and\n"+
							"run again with '--chip=%s' to skip the chip-type probe.", result)
				}
				return result, err
			}
			return withFirmware(cmd, args, probeChipType, nil, func(id string, envelopeFile *os.File, config map[string]interface{}, partitionArgs []string) error {

				sdk, err := GetSDK(ctx)
				if err != nil {
					return err
				}

				flashArguments := []string{
					"flash",
					"--port", port,
					"--baud", strconv.Itoa(int(baud)),
				}
				flashArguments = append(flashArguments, partitionArgs...)

				// Golang equivalent of #ifdef Windows.  We skip this
				// because the whole uucp group issue does not affect
				// Windows, but on the other hand Windows has strange
				// escaping rules for COM ports over 10 (COM10, COM11),
				// which we don't want to deal with.
				if os.PathSeparator != '\\' && !shouldSkipPortCheck {
					// Check the port is writable first, to avoid the confusing error
					// message from esptool in the common case where the port is owned
					// by the dialout or uucp group.
					//
					// We deliberately do NOT open the port to check this: opening (and
					// closing) a serial port toggles the DTR/RTS lines, which resets
					// boards that use the ESP32's integrated USB peripheral (for example
					// the ESP32-S2) and takes them out of the manually-entered download
					// mode before we get a chance to flash. checkPortWritable uses
					// access(2) instead, which never opens the port.
					if err := checkPortWritable(port); err != nil {
						return err
					}
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
	addPartitionTableFlag(cmd)
	return cmd
}

func ProbeChipType(ctx context.Context, port string, sdk *SDK) (string, error) {
	// Get the esptool from the SDK.
	cmd, err := sdk.EspTool(ctx, "--port", port, "chip_id")
	if err != nil {
		return "", fmt.Errorf("failed to retrieve esptool command: %w", err)
	}
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
	// The esptool emits a line like "Chip is ESP32-C3 (...)".
	// After updating the esptool, the line will be slightly different
	//   ("Connected to ESP32-C3 on ...").
	prefix := "Detecting chip type... "
	start := strings.Index(outputStr, prefix)
	for {
		if start < 0 {
			break
		}
		// Find the space or newline following the chip type.
		start += len(prefix)
		end := strings.IndexAny(outputStr[start:], " \n\r")
		if end >= 0 {
			// Extract the chip type.
			chip := outputStr[start : start+end]
			// Lower case, and remove '-'.
			chip = strings.ToLower(strings.ReplaceAll(chip, "-", ""))
			if chip == "unsupported" {
				// The esptool might first hit the "Unsupported detection protocol", but it
				// will switch to a different protocol and then manage to extract the chip type.
				// Just try again from this position.
				nextPos := strings.Index(outputStr[start+end:], prefix)
				if nextPos < 0 {
					break
				}
				start += end + nextPos
				continue
			}

			return chip, nil
		}
	}

	return "", fmt.Errorf("failed to probe chip type: unexpected output: %s\nYou can use '--chip=<chip>' to skip this probe step in the future.", outputStr)
}
