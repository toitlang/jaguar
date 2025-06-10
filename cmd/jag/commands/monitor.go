// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

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
			defer dev.Close()

			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

			// Handle signals in a separate goroutine
			go func() {
				<-signalChan
				fmt.Printf("\nInterrupt received, shutting down gracefully...\n")
				cancel()

				if dev != nil {
					dev.Close()
				}

				// Give the decoder a moment to detect the cancellation
				// If it's still running, force exit
				time.Sleep(250 * time.Millisecond)
				fmt.Printf("Exiting...\n")
				os.Exit(0)
			}()

			if !attach {
				dev.Reboot()
			}

			var logReader io.Reader = dev

			shouldProxy, err := cmd.Flags().GetBool("proxy")
			if err != nil {
				return err
			}

			if shouldProxy {
				ch1, ch2 := multiplexReader(dev)
				logReader = ch1
				go runUartProxy(dev, ch2)
			}

			scanner := bufio.NewScanner(logReader)

			envelope, err := cmd.Flags().GetString("envelope")
			if err != nil {
				return err
			}

			// Create a context-aware decoder that can be interrupted
			decoder := NewDecoder(scanner, ctx, envelope)
			done := make(chan error, 1)
			go func() {
				decoder.decode(pretty, plain)
				done <- scanner.Err()
			}()

			// Wait for either completion or context cancellation
			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				// Context was cancelled (by signal), clean up and exit
				if dev != nil {
					dev.Close()
				}
				return ctx.Err()
			}
		},
	}

	cmd.Flags().StringP("port", "p", ConfiguredPort(), "port to monitor")
	cmd.Flags().BoolP("attach", "a", false, "attach to the serial output without rebooting it")
	cmd.Flags().BoolP("force-pretty", "r", false, "force output to use terminal graphics")
	cmd.Flags().BoolP("force-plain", "l", false, "force output to use plain ASCII text")
	cmd.Flags().Uint("baud", 115200, "the baud rate for serial monitoring")
	cmd.Flags().Bool("proxy", false, "proxy the connected device to the local network")
	cmd.Flags().String("envelope", "", "name or path of the firmware envelope")
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

func (s *serialPort) Close() error {
	if s.Port != nil {
		return s.Port.Close()
	}
	return nil
}

func (s *serialPort) Reboot() {
	s.SetDTR(false)
	s.SetRTS(true)
	time.Sleep(100 * time.Millisecond)
	s.SetRTS(false)
}
