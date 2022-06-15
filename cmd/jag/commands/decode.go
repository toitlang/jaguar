// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			base64Message := args[0]
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

			if _, err := os.Stat(snapshot); errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "No such file: %s\n", snapshot)
				return fmt.Errorf("cannot find snapshot for program: %s", programId.String())
			}

			decodeCmd := sdk.ToitRun(ctx, sdk.SystemMessageSnapshotPath(), snapshot, "-b", base64Message)
			decodeCmd.Stderr = os.Stderr
			decodeCmd.Stdout = os.Stdout
			return decodeCmd.Run()
		},
	}
	return cmd
}
