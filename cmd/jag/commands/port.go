// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"go.bug.st/serial"
)

func SetPortCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "set-port",
		Short:        "Select the serial port you want to use",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := cmd.Flags().GetBool("all")
			if err != nil {
				return err
			}

			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			_, err = GetPort(cfg, all, true)
			return err
		},
	}

	cmd.Flags().Bool("all", false, "if set, will show all available ports")
	return cmd
}

func PortExists(port string) (bool, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return false, err
	}
	for _, p := range ports {
		if p == port {
			return true, nil
		}
	}
	return false, nil
}

func ConfiguredPort() string {
	cfg, err := GetConfig()
	if err != nil {
		return ""
	}
	return cfg.GetString("port")
}

func CheckPort(port string) (string, error) {
	exists, err := PortExists(port)
	if err != nil {
		return "", err
	}
	if exists {
		return port, nil
	}

	cfg, err := GetConfig()
	if err != nil {
		return "", err
	}

	return GetPort(cfg, false, true)
}

func pickPort(all bool) (string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return "", err
	}
	if !all {
		ports = filterPorts(ports)
	}
	if len(ports) == 0 {
		return "", fmt.Errorf("no serial ports detected. Have you installed the driver to the ESP32 you have connected?")
	}

	prompt := promptui.Select{
		Label:     "Choose what serial port you want to use",
		Items:     ports,
		Templates: &promptui.SelectTemplates{},
	}

	i, _, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("you didn't select anything")
	}

	return ports[i], nil
}

func filterPorts(ports []string) []string {
	switch runtime.GOOS {
	case "darwin":
		return darwinFilterPaths(ports)
	case "linux":
		return linuxFilterPaths(ports)
	default:
		return ports
	}
}

func darwinFilterPaths(paths []string) []string {
	existing := map[string]struct{}{}
	for _, p := range paths {
		existing[p] = struct{}{}
	}
	var res []string
	for _, path := range paths {
		if strings.HasPrefix(path, "/dev/cu") && !strings.Contains(path, "Bluetooth") {
			res = append(res, path)
		} else if strings.HasPrefix(path, "/dev/tty") && !strings.Contains(path, "Bluetooth") {
			candidate := "/dev/cu" + strings.TrimPrefix(path, "/dev/tty")
			if _, exists := existing[candidate]; !exists {
				res = append(res, path)
			}
		}
	}
	return res
}

func linuxFilterPaths(paths []string) []string {
	res := []string(nil)
	for _, path := range paths {
		if strings.Contains(path, "tty") {
			if strings.Contains(path, "USB") || strings.Contains(path, "ACM0") {
				res = append(res, path)
			}
		}
	}
	return res
}
