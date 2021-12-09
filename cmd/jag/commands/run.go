// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
)

func RunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run <entrypoint>",
		Short:        "run toit code",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			device, err := GetDevice(ctx, cfg, true)
			if err != nil {
				return err
			}

			toitc, err := Toitc()
			if err != nil {
				return err
			}

			toitvm, err := Toitvm()
			if err != nil {
				return err
			}

			toits2i, ok := os.LookupEnv(ToitSnap2ImagePathEnv)
			if !ok {
				return fmt.Errorf("You must set the env variable '%s'", ToitSnap2ImagePathEnv)
			}

			snapshot, err := os.CreateTemp("", "*.snap")
			if err != nil {
				return err
			}
			snapshot.Close()
			defer os.Remove(snapshot.Name())

			entrypoint := args[0]
			buildSnap := toitc.Cmd(ctx, "-w", snapshot.Name(), entrypoint)
			buildSnap.Stderr = os.Stderr
			buildSnap.Stdout = os.Stdout
			if err := buildSnap.Run(); err != nil {
				return err
			}

			image, err := os.CreateTemp("", "*.image")
			if err != nil {
				return err
			}
			image.Close()
			defer os.Remove(image.Name())

			bits := "-m32"
			if device.WordSize == 8 {
				bits = "-m64"
			}

			buildImage := toitvm.Cmd(ctx, toits2i, "--binary", bits, snapshot.Name(), image.Name())
			buildImage.Stderr = os.Stderr
			buildImage.Stdout = os.Stdout
			if err := buildImage.Run(); err != nil {
				return err
			}

			b, err := ioutil.ReadFile(image.Name())
			if err != nil {
				return err
			}
			return device.Run(ctx, b)
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "How long to scan")
	return cmd
}
