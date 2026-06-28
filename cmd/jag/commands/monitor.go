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

	"github.com/spf13/cobra"
)

func MonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor the serial output of an ESP32",
		Long: "Monitor the output of an ESP32.\n\n" +
			"With no subcommand, this defaults to 'jag monitor uart' for backwards compatibility.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runMonitorUart,
	}

	addUartFlags(cmd)
	cmd.AddCommand(MonitorUartCmd())
	cmd.AddCommand(MonitorNetworkCmd())
	return cmd
}

// cancelOnSignal calls cancel and prints message when the user hits Ctrl-C.
func cancelOnSignal(cancel context.CancelFunc, message string) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Fprintf(os.Stderr, "\n%s\n", message)
		cancel()
	}()
}

// decoderFlags holds the shared monitor output flags that configure the
// decoder, common to the uart and network monitors.
type decoderFlags struct {
	pretty   bool
	plain    bool
	envelope string
}

// parseDecoderFlags reads the shared --force-pretty / --force-plain / --envelope
// flags off cmd.
func parseDecoderFlags(cmd *cobra.Command) (decoderFlags, error) {
	pretty, err := cmd.Flags().GetBool("force-pretty")
	if err != nil {
		return decoderFlags{}, err
	}
	plain, err := cmd.Flags().GetBool("force-plain")
	if err != nil {
		return decoderFlags{}, err
	}
	envelope, err := cmd.Flags().GetString("envelope")
	if err != nil {
		return decoderFlags{}, err
	}
	return decoderFlags{pretty: pretty, plain: plain, envelope: envelope}, nil
}

// decodeReader streams reader through the monitor decoder until the program's
// output ends or ctx is canceled. It returns any error from the scanner;
// cancellation (Ctrl-C or detach) yields nil.
func decodeReader(ctx context.Context, reader io.Reader, flags decoderFlags) error {
	scanner := bufio.NewScanner(reader)
	decoder := NewDecoder(scanner, ctx, flags.envelope)
	done := make(chan error, 1)
	go func() {
		decoder.decode(flags.pretty, flags.plain)
		done <- scanner.Err()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil
	}
}
