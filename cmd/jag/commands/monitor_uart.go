// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.bug.st/serial"
)

func MonitorUartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "uart",
		Short:        "Monitor the serial (UART) output of an ESP32",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runMonitorUart,
	}
	addUartFlags(cmd)
	return cmd
}

func addUartFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("port", "p", ConfiguredPort(), "port to monitor")
	cmd.Flags().BoolP("attach", "a", false, "attach to the serial output without rebooting it")
	cmd.Flags().BoolP("force-pretty", "r", false, "force output to use terminal graphics")
	cmd.Flags().BoolP("force-plain", "l", false, "force output to use plain ASCII text")
	cmd.Flags().Uint("baud", 115200, "the baud rate for serial monitoring")
	cmd.Flags().Bool("proxy", false, "proxy the connected device to the local network")
	cmd.Flags().String("envelope", "", "name or path of the firmware envelope")
}

func runMonitorUart(cmd *cobra.Command, args []string) error {
	opts, err := parseSerialMonitorFlags(cmd, "port", "baud", true, true)
	if err != nil {
		return err
	}
	return monitorSerialPort(cmd.Context(), opts, nil)
}

// parseSerialMonitorFlags reads the serial-monitor flags off cmd into a
// serialMonitorOptions. The port and baud flag names differ between callers
// (`monitor` uses "port"/"baud", `run` uses "monitor-port"/"monitor-baud"), so
// they are passed in. hasAttach and hasProxy say whether the command exposes
// those optional flags; `run` omits both, so it always attaches in place
// (reboot stays false, since it just started the program) and never proxies.
func parseSerialMonitorFlags(cmd *cobra.Command, portFlag, baudFlag string, hasAttach, hasProxy bool) (serialMonitorOptions, error) {
	port, err := cmd.Flags().GetString(portFlag)
	if err != nil {
		return serialMonitorOptions{}, err
	}

	baud, err := cmd.Flags().GetUint(baudFlag)
	if err != nil {
		return serialMonitorOptions{}, err
	}

	reboot := false
	if hasAttach {
		attach, err := cmd.Flags().GetBool("attach")
		if err != nil {
			return serialMonitorOptions{}, err
		}
		reboot = !attach
	}

	proxy := false
	if hasProxy {
		proxy, err = cmd.Flags().GetBool("proxy")
		if err != nil {
			return serialMonitorOptions{}, err
		}
	}

	decoder, err := parseDecoderFlags(cmd)
	if err != nil {
		return serialMonitorOptions{}, err
	}

	return serialMonitorOptions{
		port:    port,
		baud:    int(baud),
		reboot:  reboot,
		proxy:   proxy,
		decoder: decoder,
	}, nil
}

// serialMonitorOptions configures monitorSerialPort.
type serialMonitorOptions struct {
	port    string
	baud    int
	reboot  bool         // pulse the reset line before reading; false attaches in place
	proxy   bool         // tee the device onto the local-network proxy
	decoder decoderFlags // shared output flags for the decoder
}

// monitorSerialPort opens the serial port and streams its output through the
// decoder until the program ends or the user detaches.
//
// afterOpen, if non-nil, runs once the port is open but before streaming starts.
// `run -m -p` uses it to send the program only after the port is listening, so
// the program's first lines aren't lost in the upload window.
func monitorSerialPort(ctx context.Context, opts serialMonitorOptions, afterOpen func() error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	port, err := CheckPort(opts.port)
	if err != nil {
		return err
	}

	fmt.Printf("Reading serial output from '%s' ...\n", port)
	dev, err := serialOpen(port, opts.baud)
	if err != nil {
		return err
	}
	defer dev.Close()

	cancelOnSignal(cancel, "Detaching...")

	if opts.reboot {
		dev.Reboot()
	}

	// When proxy is set, tee the device onto the local-network proxy and feed
	// the decoder the other tap; otherwise read the device directly.
	var logReader io.Reader = dev
	if opts.proxy {
		ch1, ch2 := multiplexReader(dev)
		go runUartProxy(dev, ch2)
		logReader = ch1
	}

	// Send the program (if any) now that the port is open; its output will be
	// buffered by the OS until decodeReader starts draining it just below.
	if afterOpen != nil {
		if err := afterOpen(); err != nil {
			return err
		}
	}

	return decodeReader(ctx, logReader, opts.decoder)
}

func serialOpen(port string, baud int) (*serialPort, error) {
	dev, err := serial.Open(port, &serial.Mode{
		BaudRate: baud,
		// Make sure we don't accidentally reset the device on open.
		InitialStatusBits: &serial.ModemOutputBits{
			RTS: true,
			DTR: true,
		},
	})
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
