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
	scanTimeoutBle     = 2 * time.Second
	scanTimeoutNetwork = 600 * time.Millisecond
	scanPort           = 1990
	scanHttpPort       = 9000
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
				devices, err := scan(ctx, timeout, port, autoSelect)
				if err != nil {
					return err
				}

				return outputter.Encode(Devices{devices})
			}

			device, _, err := scanAndPickDevice(ctx, timeout, port, autoSelect, false)
			if err != nil {
				return err
			}

			if autoSelect != nil {
				outputter = yaml.NewEncoder(os.Stdout)
				err = outputter.Encode(device)
				if err != nil {
					return err
				}
			}

			deviceCfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}
			deviceCfg.Set("device", device.ToSerializable())
			return deviceCfg.WriteConfig()
		},
	}

	cmd.Flags().BoolP("list", "l", false, "if set, list the devices")
	cmd.Flags().StringP("output", "o", "short", "set output format to json, yaml or short (works only with '--list')")
	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on (ignored when an address is given)")
	cmd.Flags().DurationP("timeout", "t", 0, "how long to scan")
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

type deviceBLEAddressSelect string

func (s deviceBLEAddressSelect) Match(d Device) bool {
	return string(s) == d.Address()
}

func (s deviceBLEAddressSelect) Address() string {
	return string(s)
}

func (s deviceBLEAddressSelect) String() string {
	return fmt.Sprintf("BLE device with address: '%s'", string(s))
}

func scan(ctx context.Context, scanTimeout time.Duration, port uint, autoSelect deviceSelect) ([]Device, error) {
	userCfg, err := directory.GetUserConfig()
	if err != nil {
		return nil, err
	}
	bleIsEnabled := userCfg.GetBool("ble.enabled")

	description := "network"
	if bleIsEnabled {
		description += " and BLE"
	}
	if autoSelect == nil {
		fmt.Printf("Scanning %s ...\n", description)
	} else {
		fmt.Printf("Scanning %s for %s\n", description, autoSelect)
	}

	if scanTimeout == 0 {
		if bleIsEnabled {
			scanTimeout = scanTimeoutBle
		} else {
			scanTimeout = scanTimeoutNetwork
		}
	}

	bleCh := make(chan []Device)
	errCh := make(chan error, 1) // Buffered to hold one error.

	if bleIsEnabled {
		go func() {
			scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
			defer cancel()
			bleDevices, err := ScanBle(scanCtx, autoSelect)
			if err != nil {
				errCh <- err
				return
			}
			bleCh <- bleDevices
		}()
	} else {
		bleCh <- []Device{}
	}
	scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	devices, err := ScanNetwork(scanCtx, autoSelect, port)
	if err != nil {
		return nil, err
	}
	// Wait for the BLE scan to finish.
	select {
	case err := <-errCh:
		return nil, err
	case bleDevices := <-bleCh:
		devices = append(devices, bleDevices...)
	}

	return devices, nil
}

func scanAndPickDevice(ctx context.Context, scanTimeout time.Duration, port uint, autoSelect deviceSelect, manualPick bool) (Device, bool, error) {
	devices, err := scan(ctx, scanTimeout, port, autoSelect)

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
