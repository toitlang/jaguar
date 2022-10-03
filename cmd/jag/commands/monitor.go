// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.bug.st/serial"
)

func MonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "monitor",
		Short:        "Monitor the serial output of an ESP32",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			port, err := cmd.Flags().GetString("port")
			if err != nil {
				return err
			}

			if port, err = CheckPort(port); err != nil {
				return err
			}

			baud, err := cmd.Flags().GetUint("baud")
			if err != nil {
				return err
			}

			attach, err := cmd.Flags().GetBool("attach")
			if err != nil {
				return err
			}

			pretty, err := cmd.Flags().GetBool("force-pretty")
			if err != nil {
				return err
			}

			plain, err := cmd.Flags().GetBool("force-plain")
			if err != nil {
				return err
			}

			fmt.Printf("Starting serial monitor of port '%s' ...\n", port)
			dev, err := serialOpen(port, &serial.Mode{
				BaudRate: int(baud),
			})
			if err != nil {
				return err
			}

			if !attach {
				dev.Reboot()
			}

			scanner := bufio.NewScanner(dev)

			decoder := Decoder{scanner, cmd}

			decoder.decode(pretty, plain)

			return scanner.Err()
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "port to monitor")
	cmd.Flags().BoolP("attach", "a", false, "attach to the serial output without rebooting it")
	cmd.Flags().BoolP("force-pretty", "r", false, "Force output to use terminal graphics")
	cmd.Flags().BoolP("force-plain", "l", false, "Force output to use plain ASCII text")
	cmd.Flags().Uint("baud", 115200, "the baud rate for serial monitoring")
	return cmd
}

func serialOpen(port string, mode *serial.Mode) (*serialPort, error) {
	dev, err := serial.Open(port, mode)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("the port '%s' was not found", port)
	}
	if err != nil {
		return nil, err
	}

	return &serialPort{dev}, err
}

type serialPort struct {
	serial.Port
}

func (s serialPort) Read(buf []byte) (n int, err error) {
	n, err = s.Port.Read(buf)
	if err == nil && n == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}

func (s *serialPort) Reboot() {
	s.SetDTR(false)
	s.SetRTS(true)
	time.Sleep(100 * time.Millisecond)
	s.SetRTS(false)
}
