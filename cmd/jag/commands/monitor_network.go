// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func MonitorNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "network",
		Short:        "Monitor the output of an ESP32 over the network",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runMonitorNetwork,
	}
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	cmd.Flags().StringArray("container", nil, "only show output from the named container (repeatable; '_run_' matches anonymous 'run' programs)")
	cmd.Flags().BoolP("force-pretty", "r", false, "force output to use terminal graphics")
	cmd.Flags().BoolP("force-plain", "l", false, "force output to use plain ASCII text")
	cmd.Flags().String("envelope", "", "name or path of the firmware envelope")
	cmd.Flags().Uint("log-buffer-size", 0, "resize the device log buffer to this many bytes (default: leave unchanged)")
	cmd.Flags().String("min-log-level", "", "minimum level captured in the device log buffer (TRACE, DEBUG, INFO, WARN, ERROR, FATAL; default: leave unchanged)")
	return cmd
}

func runMonitorNetwork(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	deviceSelect, err := parseDeviceFlag(cmd)
	if err != nil {
		return err
	}

	containers, err := cmd.Flags().GetStringArray("container")
	if err != nil {
		return err
	}

	sdk, err := GetSDK(ctx)
	if err != nil {
		return err
	}

	device, err := GetDevice(ctx, sdk, true, deviceSelect)
	if err != nil {
		return err
	}

	// Monitoring over the network always enables capture so the device records
	// output into its log ring. Only resize the buffer or change the min level
	// when asked; otherwise leave the device's current settings untouched.
	logConfig := map[string]interface{}{"enabled": true}
	if cmd.Flags().Changed("log-buffer-size") {
		bufferSize, err := cmd.Flags().GetUint("log-buffer-size")
		if err != nil {
			return err
		}
		logConfig["buffer_size"] = bufferSize
	}
	if cmd.Flags().Changed("min-log-level") {
		minLevel, err := cmd.Flags().GetString("min-log-level")
		if err != nil {
			return err
		}
		logConfig["min_level"] = minLevel
	}
	if err := device.ConfigureLog(ctx, logConfig); err != nil {
		return err
	}

	// The standalone monitor keeps watching across program exits/restarts and
	// replays whatever is already buffered (startCursor 0); only `run -m`
	// (monitorRunNetwork) stops once its run is done and starts past the buffer.
	return monitorNetwork(cmd, device, containers, false, 0)
}

// logEntryTypeExit is the entry type the device uses to mark that a captured
// container stopped; its text is the exit code.
const logEntryTypeExit = "exit"

// logHead returns the device's current log head (the latest captured seq). Seqs
// are global across containers, so a later poll starting from this value skips
// whatever is already buffered and shows only output produced afterwards.
func logHead(ctx context.Context, device Device) (int, error) {
	// No container filter: head is global, and matching any container lets the
	// device's long-poll return at once if the ring holds anything at all.
	resp, err := device.PollLog(ctx, 0, nil)
	if err != nil {
		return 0, err
	}
	return resp.Head, nil
}

// monitorRunNetwork polls GET /log filtered to the given container names and
// feeds this run's output through the shared decoder until the program exits or
// the user detaches. startCursor is the seq to start past (see logHead).
func monitorRunNetwork(cmd *cobra.Command, device Device, containers []string, startCursor int) error {
	return monitorNetwork(cmd, device, containers, true, startCursor)
}

// monitorNetwork polls GET /log filtered to the given container names and feeds
// the output through the shared decoder until the user detaches. It starts from
// startCursor (0 replays the whole buffer). When stopOnExit is set it also
// returns once a captured container exits (used by `run -m`); otherwise it keeps
// watching across exits and restarts.
func monitorNetwork(cmd *cobra.Command, device Device, containers []string, stopOnExit bool, startCursor int) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	cancelOnSignal(cancel, "Detaching (program keeps running on the device)...")

	flags, err := parseDecoderFlags(cmd)
	if err != nil {
		return err
	}

	// The poll loop writes reconstructed lines into the pipe; decodeReader reads
	// them out the other end and decodes them, exactly like the serial monitor.
	// Propagate the poll loop's error (e.g. lost connection) to the decoder side
	// via the pipe; a nil error closes it with plain EOF.
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(pollLogLoop(ctx, device, containers, writer, stopOnExit, startCursor))
	}()
	return decodeReader(ctx, reader, flags)
}

func pollLogLoop(ctx context.Context, device Device, containers []string, out io.Writer, stopOnExit bool, startCursor int) error {
	cursor := startCursor
	firstPoll := true
	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return nil // Detached.
		default:
		}

		resp, err := device.PollLog(ctx, cursor, containers)
		if err != nil {
			if ctx.Err() != nil {
				return nil // Canceled while polling.
			}
			consecutiveErrors++
			if consecutiveErrors > 20 {
				return fmt.Errorf("lost connection to device: %w", err)
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
		consecutiveErrors = 0

		// Drop detection (filter-agnostic): if entries we had not read yet were
		// evicted from the ring, note it. Skip on the first poll (initial sync).
		if !firstPoll && cursor < resp.Oldest-1 {
			dropped := resp.Oldest - 1 - cursor
			fmt.Fprintf(out, "[... %d line(s) dropped ...]\n", dropped)
		}
		firstPoll = false

		for _, entry := range resp.Entries {
			if entry.Type == logEntryTypeExit {
				// Emit the marker inline at the exit's real position so several
				// buffered exits (e.g. a cursor-0 replay of past runs) each show
				// up where they happened instead of collapsing into one.
				exitCode, _ := strconv.Atoi(entry.Text)
				fmt.Fprintf(out, "[ program stopped - exit code %d ]\n", exitCode)
				// `run -m` watches a single program, so it stops at the first
				// exit; the standalone monitor keeps following across restarts.
				if stopOnExit {
					return nil
				}
				continue
			}
			fmt.Fprintf(out, "%s\n", entry.Text)
		}
		// Advance by Next, not Head: the device caps each response, so Next may
		// trail Head; the next poll then pages through the rest of the backlog.
		cursor = resp.Next
	}
}
