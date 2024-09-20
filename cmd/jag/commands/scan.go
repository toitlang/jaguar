// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"github.com/toitware/ubjson"
	"gopkg.in/yaml.v2"
)

const (
	scanTimeout  = 600 * time.Millisecond
	scanPort     = 1990
	scanHttpPort = 9000
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
				scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
				var devices []Device
				var err error
				devices, err = scan(scanCtx, autoSelect, port)
				cancel()
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

			cfg.Set("device", device.ToJson())
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

func scanAndPickDevice(ctx context.Context, scanTimeout time.Duration, port uint, autoSelect deviceSelect, manualPick bool) (Device, bool, error) {
	if autoSelect == nil {
		fmt.Println("Scanning ...")
	} else {
		fmt.Println("Scanning for ", autoSelect)
	}
	scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
	devices, err := scan(scanCtx, autoSelect, port)
	cancel()
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

func scan(ctx context.Context, ds deviceSelect, port uint) ([]Device, error) {
	if ds != nil && ds.Address() != "" {
		addr := ds.Address()
		if !strings.Contains(addr, ":") {
			addr = addr + ":" + fmt.Sprint(scanHttpPort)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/identify", nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		buf, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("got non-OK from device: %s", res.Status)
		}
		dev, err := parseDeviceNetwork(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse identify. reason %w", err)
		} else if dev == nil {
			return nil, fmt.Errorf("invalid identify response")
		}
		return []Device{*dev}, nil
	}

	pc, err := reuseport.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := pc.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	devices := map[string]Device{}
looping:
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if err == context.DeadlineExceeded {
				break looping
			}
			return nil, err
		default:
		}

		buf := make([]byte, 1024)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if isTimeoutError(err) {
				break looping
			}
			return nil, err
		}

		dev, err := parseDeviceNetwork(buf[:n])
		if err != nil {
			fmt.Println("Failed to parse identify", err)
		} else if dev != nil {
			devices[dev.Address()] = *dev
		}
	}

	var res []Device
	for _, d := range devices {
		res = append(res, d)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name() < res[j].Name() })
	return res, nil
}

type udpMessage struct {
	Method  string                 `json:"method"`
	Payload map[string]interface{} `json:"payload"`
}

func parseDeviceNetwork(bytes []byte) (*DeviceNetwork, error) {
	var msg udpMessage
	if err := ubjson.Unmarshal(bytes, &msg); err != nil {
		if err := json.Unmarshal(bytes, &msg); err != nil {
			return nil, fmt.Errorf("could not parse message: %s. Reason: %w", string(bytes), err)

		}
	}

	if msg.Method != "jaguar.identify" {
		return nil, nil
	}

	return NewDeviceNetworkFromJson(msg.Payload)
}

func isTimeoutError(err error) bool {
	e, ok := err.(net.Error)
	return ok && e.Timeout()
}
