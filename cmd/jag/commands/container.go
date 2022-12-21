// Copyright (C) 2022 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func ContainerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container",
		Short: "Manipulate the set of installed containers on a device",
		Long: "Manipulate the set of installed containers on a device.\n" +
			"Installed containers run on boot and are primarily used to provide\n" +
			"services and drivers to applications.",
	}

	cmd.AddCommand(ContainerListCmd())
	cmd.AddCommand(ContainerInstallCmd())
	cmd.AddCommand(ContainerUninstallCmd())
	return cmd
}

func ContainerListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "list",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, false, deviceSelect)
			if err != nil {
				return err
			}

			containers, err := device.ContainerList(ctx, sdk)
			if err != nil {
				return err
			}

			// Compute the column lengths for all columns except for the last.
			deviceNameLength := max(len("DEVICE"), len(device.Name))
			idLength := len("IMAGE")
			for id := range containers {
				idLength = max(idLength, len(id))
			}

			fmt.Println(padded("DEVICE", deviceNameLength) + padded("IMAGE", idLength) + "NAME")
			for id, name := range containers {
				fmt.Println(padded(device.Name, deviceNameLength) + padded(id, idLength) + name)
			}
			return nil
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	return cmd
}

func ContainerInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "install <name> <file>",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			programAssetsPath, err := GetProgramAssetsPath(cmd.Flags(), "assets")
			if err != nil {
				return err
			}

			entrypoint := args[1]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't run directory: '%s'", entrypoint)
			}

			optimizationLevel := -1
			if cmd.Flags().Changed("optimization-level") {
				optimizationLevel, err = cmd.Flags().GetInt("optimization-level")
				if err != nil {
					return err
				}
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			name := args[0]
			defines, err := parseDefineFlags(cmd, "define")
			if err != nil {
				return err
			}

			return InstallFile(cmd, device, sdk, name, entrypoint, defines, programAssetsPath, optimizationLevel)
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	cmd.Flags().StringArrayP("define", "D", nil, "define settings to control container on device")
	cmd.Flags().String("assets", "", "attach assets to the container")
	cmd.Flags().IntP("optimization-level", "O", -1, "optimization level")
	return cmd
}

func ContainerUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "uninstall <name>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			cfg, err := directory.GetDeviceConfig()
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

			name := args[0]
			fmt.Printf("Uninstalling container '%s' on '%s' ...\n", name, device.Name)
			return device.ContainerUninstall(ctx, sdk, name)
		},
	}

	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	return cmd
}

func padded(prefix string, total int) string {
	return prefix + strings.Repeat(" ", 3+total-len(prefix))
}

func max(x int, y int) int {
	if x > y {
		return x
	} else {
		return y
	}
}
