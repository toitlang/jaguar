// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func PingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Ping a Jaguar device to see if it's active",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			ctx := context.Background()
			device, err := GetDevice(ctx, cfg, false)
			if err != nil {
				return err
			}
			if !device.Ping() {
				cmd.SilenceUsage = true
				return fmt.Errorf("couldn't ping the device")
			}

			fmt.Println("got ping from the device")
			return nil
		},
	}

	cmd.Flags().DurationP("timeout", "t", pingTimeout, "How long to wait for a reply")
	return cmd
}
