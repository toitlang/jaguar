// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"github.com/toitware/ubjson"
)

func DecodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decode <message>",
		Short: "Decode a stack trace received from a Jaguar device",
		Long: "Decode a stack trace received from a Jaguar device. Stack traces are encoded\n" +
			"using base64 and are easy to copy from the serial output.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return serialDecode(cmd, args[0])
		},
	}
	return cmd
}

func serialDecode(cmd *cobra.Command, message string) error {
	if strings.HasPrefix(message, "jag decode ") {
		return jagDecode(cmd, message[11:])
	} else if strings.HasPrefix(message, "Backtrace:") {
		return crashDecode(cmd, message)
	} else {
		return jagDecode(cmd, message)
	}
}

func jagDecode(cmd *cobra.Command, base64Message string) error {
	ctx := cmd.Context()
	sdk, err := GetSDK(ctx)
	if err != nil {
		return err
	}

	equalsIndex := strings.Index(base64Message, "=")
	if equalsIndex != -1 && !strings.HasSuffix(base64Message, "=") {
		// The = symbols that optionally indicate the end of the base64
		// encoding are not at the end.  Let's trim the junk off the end.
		if base64Message[equalsIndex+1] == '=' {
			// There might be two = signs at the end.
			equalsIndex++
		}
		base64Message = base64Message[0 : equalsIndex+1]
	} else {
		// Try to trim, based on the first index of something that is not
		// allowed in base64 encoding.
		i := 0
		for ; i < len(base64Message); i++ {
			c := base64Message[i]
			if 'a' <= c && c <= 'z' {
				continue
			}
			if 'A' <= c && c <= 'Z' {
				continue
			}
			if '0' <= c && c <= '9' {
				continue
			}
			if c == '+' || c == '/' || c == '=' {
				continue
			}
			break
		}
		base64Message = base64Message[0:i]
	}

	message, err := base64.StdEncoding.DecodeString(base64Message)
	if err != nil {
		return err
	}

	var decoded []interface{}
	if err = ubjson.Unmarshal(message, &decoded); err != nil {
		return fmt.Errorf("failed to parse message as ubjson, reason: %v", err)
	}

	if len(decoded) != 4 && len(decoded) != 5 {
		return fmt.Errorf("message did not have correct format")
	}

	i := 0
	if v, ok := decoded[i].(int64); !ok || rune(v) != 'X' {
		return fmt.Errorf("message did not have correct format")
	}
	i++

	_, ok := decoded[i].(string)
	if !ok {
		return fmt.Errorf("message did not have correct format")
	}
	i++

	if len(decoded) == 5 {
		if _, ok := decoded[i].(string); !ok {
			return fmt.Errorf("message did not have correct format")
		}
		i++
	}

	var programIdBytes []byte
	if mapstructure.Decode(decoded[i], &programIdBytes) != nil {
		return fmt.Errorf("message did not have correct format")
	}

	programId, err := uuid.FromBytes(programIdBytes)
	if err != nil {
		return fmt.Errorf("failed to parse program id: %v", err)
	}

	snapshotsCache, err := directory.GetSnapshotsCachePath()
	if err != nil {
		return err
	}
	snapshot := filepath.Join(snapshotsCache, programId.String()+".snapshot")

	var decodeCommand *exec.Cmd
	if programId != uuid.Nil {
		if _, err := os.Stat(snapshot); errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "No such file: %s\n", snapshot)
			return fmt.Errorf("cannot find snapshot for program: %s", programId.String())
		}
		decodeCommand = sdk.SystemMessage(ctx, "--snapshot", snapshot, "--message", base64Message)
	} else {
		decodeCommand = sdk.SystemMessage(ctx, "--message", base64Message)
	}

	decodeCommand.Stderr = os.Stderr
	decodeCommand.Stdout = os.Stdout
	return decodeCommand.Run()
}

func crashDecode(cmd *cobra.Command, backtrace string) error {
	ctx := cmd.Context()
	sdk, err := GetSDK(ctx)
	if err != nil {
		return err
	}

	envelopePath, err := directory.GetFirmwareEnvelopePath()
	if err != nil {
		return err
	}

	firmwareElf, err := ExtractFirmwarePart(ctx, sdk, envelopePath, "firmware.elf")
	if err != nil {
		return err
	}
	defer firmwareElf.Close()

	objdump, err := exec.LookPath("xtensa-esp32-elf-objdump")
	if err != nil {
		objdump, err = exec.LookPath("objdump")
	}
	if err != nil {
		return err
	}
	stacktraceCommand := sdk.Stacktrace(ctx, "--objdump", objdump, "--backtrace", backtrace, firmwareElf.Name())
	stacktraceCommand.Stderr = os.Stderr
	stacktraceCommand.Stdout = os.Stdout
	fmt.Println("Crash in native code:")
	fmt.Println(backtrace)
	return stacktraceCommand.Run()
}

type Decoder struct {
	scanner *bufio.Scanner
	cmd     *cobra.Command
}

func (d *Decoder) decode() {
	POSTPONED_LINES := map[string]bool{
		"----": true,
		"Received a Toit system message. Executing the command below will": true,
		"make it human readable:": true,
	}

	Version := ""

	postponed := []string{}

	for d.scanner.Scan() {
		// Get next line from device (or simulator) console.
		line := d.scanner.Text()
		versionPrefix := "[toit] INFO: starting <v"
		if strings.HasPrefix(line, versionPrefix) && strings.HasSuffix(line, ">") {
			Version = line[len(versionPrefix) : len(line)-1]
		}
		if _, contains := POSTPONED_LINES[line]; contains {
			postponed = append(postponed, line)
		} else {
			separator := strings.Repeat("*", 78)
			if strings.HasPrefix(line, "jag decode ") || strings.HasPrefix(line, "Backtrace:") {
				fmt.Printf("\n" + separator + "\n")
				if Version != "" {
					fmt.Printf("Decoding by `jag`, device has version <%s>\n", Version)
					fmt.Printf(separator + "\n")
				}
				if err := serialDecode(d.cmd, line); err != nil {
					if len(postponed) != 0 {
						fmt.Println(strings.Join(postponed, "\n"))
						postponed = []string{}
					}
					fmt.Println(line)
					fmt.Println("jag: Failed to decode line.")
				} else {
					postponed = []string{}
				}
				fmt.Printf(separator + "\n\n")
			} else {
				if len(postponed) != 0 {
					fmt.Println(strings.Join(postponed, "\n"))
					postponed = []string{}
				}
				fmt.Println(line)
			}
		}
	}
}
