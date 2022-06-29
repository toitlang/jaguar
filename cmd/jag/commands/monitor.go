// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
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
			Version := ""
			POSTPONED_LINES := map[string]bool{
				"----": true,
				"Received a Toit stack trace. Executing the command below will": true,
				"make it human readable:": true,
			}

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

			postponed := []string{}

			for scanner.Scan() {
				// Get next line from serial port.
				line := scanner.Text()
				versionPrefix := "[toit] INFO: starting <v"
				if strings.HasPrefix(line, versionPrefix) && strings.HasSuffix(line, ">") {
					Version = line[len(versionPrefix) : len(line)-1]
				}
				if _, contains := POSTPONED_LINES[line]; contains {
					postponed = append(postponed, line)
				} else {
					separator := strings.Repeat("*", 78)
					if strings.HasPrefix(line, "jag decode ") || strings.HasPrefix(line, "Backtrace:") {
						fmt.Printf("\n" + separator + "\n")
						if Version != "" {
							fmt.Printf("Decoded by `jag monitor` <%s>\n", Version)
							fmt.Printf(separator + "\n")
						}
						if err := serialDecode(cmd, line); err != nil {
							if len(postponed) != 0 {
								fmt.Println(strings.Join(postponed, "\n"))
								postponed = []string{}
							}
							fmt.Println(line)
							fmt.Println("jag monitor: Failed to decode line.")
						} else {
							postponed = []string{}
						}
						fmt.Printf(separator + "\n\n")
					} else {
						if len(postponed) != 0 {
							fmt.Println(strings.Join(postponed, "\n"))
							postponed = []string{}
						}
						fmt.Println(line)
					}
				}
			}

			return scanner.Err()
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "port to monitor")
	cmd.Flags().BoolP("attach", "a", false, "attach to the serial output without rebooting it")
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
