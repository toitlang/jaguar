// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"gopkg.in/yaml.v2"
)

const (
	scanTimeout     = 600 * time.Millisecond
	identifyTimeout = 1000 * time.Millisecond
	scanPort        = 1990
	scanHttpPort    = 9000
)

func ScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [device-name-or-IP]",
		Short: "Scan for Jaguar devices",
		Long: "Scan for Jaguar devices.\n" +
			"Unless 'device' is an address, listen for UDP packets broadcasted by the devices.\n" +
			"In that case you need to be on the same network as the device.\n" +
			"If a device selection is given, automatically select that device.\n" +
			"If the device selection is an address, connect to it using TCP.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			var autoSelect deviceSelect = nil
			if len(args) == 1 {
				autoSelect = parseDeviceSelection(args[0])
			}

			port, err := cmd.Flags().GetUint("port")
			if err != nil {
				return err
			}

			timeout, err := cmd.Flags().GetDuration("timeout")
			if err != nil {
				return err
			}

			outputter, err := parseOutputFlag(cmd)
			if err != nil {
				return err
			}

			if outputter != nil && autoSelect != nil {
				return fmt.Errorf("listing and device-selection are exclusive")
			}

			cmd.SilenceUsage = true
			if outputter != nil {
				var devices []Device
				var err error
				scanCtx, cancel := context.WithTimeout(ctx, timeout)
				devices, err = ScanNetwork(scanCtx, autoSelect, port)
				cancel()
				if err != nil {
					return err
				}

				return outputter.Encode(Devices{devices})
			}

			identifyTimeout := identifyTimeout
			if userCfg, err := directory.GetUserConfig(); err == nil && userCfg.IsSet(IdentifyTimeoutCfgKey) {
				timeout := userCfg.GetString(IdentifyTimeoutCfgKey)
				if d, err := time.ParseDuration(timeout); err == nil {
					identifyTimeout = d
				}
			}

			device, _, err := scanAndPickDevice(ctx, timeout, identifyTimeout, port, autoSelect, false)
			if err != nil {
				return err
			}

			json := device.ToJson()
			if autoSelect != nil {
				outputter = yaml.NewEncoder(os.Stdout)
				err = outputter.Encode(json)
				if err != nil {
					return err
				}
			}

			cfg.Set("device", json)
			return cfg.WriteConfig()
		},
	}

	cmd.Flags().BoolP("list", "l", false, "if set, list the devices")
	cmd.Flags().StringP("output", "o", "short", "set output format to json, yaml or short (works only with '--list')")
	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on (ignored when an address is given)")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "how long to scan")
	return cmd
}

type deviceSelect interface {
	Match(d Device) bool
	Address() string
}

type deviceIDSelect string

func (s deviceIDSelect) Match(d Device) bool {
	return string(s) == d.ID()
}

func (s deviceIDSelect) Address() string {
	return ""
}

func (s deviceIDSelect) String() string {
	return fmt.Sprintf("device with ID: '%s'", string(s))
}

type deviceNameSelect string

func (s deviceNameSelect) Match(d Device) bool {
	return string(s) == d.Name()
}

func (s deviceNameSelect) Address() string {
	return ""
}

func (s deviceNameSelect) String() string {
	return fmt.Sprintf("device with name: '%s'", string(s))
}

type deviceAddressSelect string

func (s deviceAddressSelect) Match(d Device) bool {
	// The device address contains the 'http://' prefix and a port number.
	m := string(s)
	if !strings.HasPrefix(m, "http://") {
		m = "http://" + m
	}
	if !strings.Contains(m, ":") {
		m += ":"
	}
	return strings.HasPrefix(d.Address(), m)
}

func (s deviceAddressSelect) Address() string {
	if strings.HasPrefix(string(s), "http://") {
		// Trim the 'http://' prefix.
		return string(s[7:])
	}
	return string(s)
}

func (s deviceAddressSelect) String() string {
	return fmt.Sprintf("device with address: '%s'", string(s))
}

func scanAndPickDevice(ctx context.Context, scanTimeout time.Duration, identifyTimeout time.Duration, port uint, autoSelect deviceSelect, manualPick bool) (Device, bool, error) {
	if autoSelect == nil {
		fmt.Println("Scanning ...")
	} else {
		fmt.Println("Scanning for", autoSelect)
	}
	var devices []Device
	var err error
	if autoSelect != nil && autoSelect.Address() != "" {
		identifyCtx, cancel := context.WithTimeout(ctx, identifyTimeout)
		devices, err = Identify(identifyCtx, autoSelect)
		cancel()
	} else {
		scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
		devices, err = ScanNetwork(scanCtx, autoSelect, port)
		cancel()
	}
	if err != nil {
		return nil, false, err
	}

	if len(devices) == 0 {
		return nil, false, fmt.Errorf("didn't find any Jaguar devices.\nPerhaps you need to be on the same wifi as the device.\nYou can also specify the IP address of the device")
	}
	if autoSelect != nil {
		for _, d := range devices {
			if autoSelect.Match(d) {
				return d, true, nil
			}
		}
		if manualPick {
			return nil, false, fmt.Errorf("couldn't find %s", autoSelect)
		}
	}

	prompt := promptui.Select{
		Label:     "Choose what Jaguar device you want to use",
		Items:     devices,
		Templates: &promptui.SelectTemplates{},
	}

	i, _, err := prompt.Run()
	if err != nil {
		return nil, false, fmt.Errorf("you didn't select anything")
	}

	res := devices[i]
	return res, false, nil
}
