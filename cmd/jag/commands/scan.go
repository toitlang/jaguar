// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

const (
	scanTimeout = 600 * time.Millisecond
	scanPort    = 1990
)

func ScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for Jaguar devices",
		Long: "Scan for Jaguar devices by listening for UDP packets broadcasted by the devices.\n" +
			"To use a Jaguar device, you need to be on the same network as the device.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			port, err := cmd.Flags().GetUint("port")
			if err != nil {
				return err
			}

			timeout, err := cmd.Flags().GetDuration("timeout")
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true

			device, err := scanAndPickDevice(cmd.Context(), timeout, port, nil)
			if err != nil {
				return err
			}
			cfg.Set("device", device)
			return cfg.WriteConfig()
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "how long to scan")
	return cmd
}

func scanAndPickDevice(ctx context.Context, scanTimeout time.Duration, port uint, autoSelectDeviceID *string) (*Device, error) {
	fmt.Println("Scanning ...")
	scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
	devices, err := scan(scanCtx, port)
	cancel()
	if err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("didn't find any Jaguar devices")
	}
	if autoSelectDeviceID != nil {
		for _, d := range devices {
			if d.ID == *autoSelectDeviceID {
				fmt.Printf("Found device '%s' again\n", d.Name)
				return &d, nil
			}
		}
	}

	prompt := promptui.Select{
		Label:     "Choose what Jaguar device you want to use",
		Items:     devices,
		Templates: &promptui.SelectTemplates{},
	}

	i, _, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("you didn't select anything")
	}

	res := devices[i]
	return &res, nil
}

func scan(ctx context.Context, port uint) ([]Device, error) {
	pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
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

		dev, err := parseDevice(buf[:n])
		if err != nil {
			fmt.Println("Failed to parse identify", err)
		} else if dev != nil {
			devices[dev.Address] = *dev
		}
	}

	var res []Device
	for _, d := range devices {
		res = append(res, d)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

type udpMessage struct {
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

func parseDevice(b []byte) (*Device, error) {
	var res Device

	var msg udpMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("could not parse message: %s. Reason: %w", string(b), err)
	}
	if msg.Method != "jaguar.identify" {
		return nil, nil
	}

	if err := json.Unmarshal(msg.Payload, &res); err != nil {
		return nil, fmt.Errorf("failed to parse payload of jaguar.identify: %s. reason: %w", string(b), err)
	}
	return &res, nil
}

func isTimeoutError(err error) bool {
	e, ok := err.(net.Error)
	return ok && e.Timeout()
}
