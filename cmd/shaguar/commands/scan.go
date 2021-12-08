package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sort"
	"time"

	"regexp"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

const (
	scanTimeout = 400 * time.Millisecond
	scanPort    = 1990
)

func ScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for Shaguar devices",
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

			device, err := scanAndPickDevice(context.Background(), timeout, port)
			if err != nil {
				return err
			}
			cfg.Set("device", device)
			return cfg.WriteConfig()
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP Port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "How long to scan")
	return cmd
}

func scanAndPickDevice(ctx context.Context, scanTimeout time.Duration, port uint) (*Device, error) {
	fmt.Println("scanning...")
	scanCtx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	devices, err := scan(scanCtx, port)
	cancel()
	if err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("didn't find any shaguar devices")
	}

	prompt := promptui.Select{
		Label:     "Choose what shaguar device you want to use",
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
			return nil, err
		}

		if bytes.HasPrefix(buf[:n], []byte("shaguar.identify")) {
			dev, err := parseDevice(buf[:n])
			if err != nil {
				fmt.Println("Failed to parse identify", err)
			} else {
				devices[dev.Name] = dev
			}
		} else {
			fmt.Println("got random message", string(buf[:n]))
		}
	}

	var res []Device
	for _, d := range devices {
		res = append(res, d)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

var matchDeviceRegexp = regexp.MustCompile("shaguar\\.identify\nname: ([^\n]+)\naddress: ([^\n]+)\n")

func parseDevice(b []byte) (Device, error) {
	var res Device
	if !matchDeviceRegexp.Match(b) {
		return res, fmt.Errorf("message did not match regexp: %s", base64.RawStdEncoding.EncodeToString(b))
	}
	matches := matchDeviceRegexp.FindSubmatch(b)
	res.Name = string(matches[1])
	res.Address = string(matches[2])
	return res, nil
}
