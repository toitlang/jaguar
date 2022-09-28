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
	"github.com/spf13/viper"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"go.bug.st/serial"
)

func PortCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "port",
		Short:        "Get current port or list available ports",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := cmd.Flags().GetBool("all")
			if err != nil {
				return err
			}

			outputter, err := parseOutputFlag(cmd)
			if err != nil {
				return err
			}

			if outputter != nil {
				ports, err := getPorts(all)
				if err != nil {
					return err
				}
				return outputter.Encode(ports)
			}

			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			if !cfg.IsSet("port") {
				return fmt.Errorf("port was not set, use 'jag port set' to pick a port")
			}
			fmt.Println(cfg.GetString("port"))
			return nil
		},
	}

	cmd.AddCommand(PortSetCmd())
	cmd.Flags().BoolP("list", "l", false, "If set, list the ports")
	cmd.Flags().StringP("output", "o", "short", "Set output format to json, yaml or short (works only with '--list')")
	cmd.Flags().Bool("all", false, "if set, will show all available ports")
	return cmd
}

func PortSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "set [<port>]",
		Short:        "Select the serial port you want to use",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := cmd.Flags().GetBool("all")
			if err != nil {
				return err
			}

			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			if len(args) == 1 {
				cfg.Set("port", args[0])
				return cfg.WriteConfig()
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
	cfg, err := directory.GetDeviceConfig()
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

	cfg, err := directory.GetDeviceConfig()
	if err != nil {
		return "", err
	}

	return GetPort(cfg, false, true)
}

func pickPort(all bool) (string, error) {
	ports, err := getPorts(all)
	if err != nil || ports.Len() == 0 {
		return "", fmt.Errorf("no serial ports detected. Have you installed the driver for the ESP32 you have connected?")
	}

	prompt := promptui.Select{
		Label:     "Choose what serial port you want to use",
		Items:     ports.Elements(),
		Templates: &promptui.SelectTemplates{},
	}

	i, _, err := prompt.Run()
	if err != nil {
		fmt.Println("Error", err)
		return "", fmt.Errorf("you didn't select anything")
	}

	return string(ports.Ports[i]), nil
}

func getPorts(all bool) (Ports, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return Ports{}, err
	}
	if !all {
		ports = filterPorts(ports)
	}

	var res Ports
	for _, p := range ports {
		res.Ports = append(res.Ports, Port(p))
	}
	return res, nil
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

func GetPort(cfg *viper.Viper, all bool, reset bool) (string, error) {
	if !reset && cfg.IsSet("port") {
		port := cfg.GetString("port")
		exists, err := PortExists(port)
		if err != nil {
			return "", err
		}
		if exists {
			return port, nil
		}
	}

	port, err := pickPort(all)
	if err != nil {
		return "", err
	}
	cfg.Set("port", port)
	if err := cfg.WriteConfig(); err != nil {
		return "", err
	}
	return port, nil
}

type Ports struct {
	Ports []Port `mapstructure:"ports" yaml:"ports" json:"ports"`
}

type Port string

func (p Port) Short() string {
	return string(p)
}

func (p Port) String() string {
	return string(p)
}

func (p Ports) Len() int {
	return len(p.Ports)
}

func (p Ports) Elements() []Short {
	var res []Short
	for _, p := range p.Ports {
		res = append(res, p)
	}
	return res
}
