// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func WatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "watch <directory> <entrypoint>",
		Short:        "watches <directory> for changes and runs <entrypoint> on the device every time",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			device, err := GetDevice(ctx, cfg, true)
			if err != nil {
				return err
			}

			sdk, err := GetSDK()
			if err != nil {
				return err
			}

			directory := args[0]
			if stat, err := os.Stat(directory); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("the directory '%s' did not exists", directory)
				}
				return fmt.Errorf("could not stat directory '%s', reason: %w", directory, err)
			} else if !stat.IsDir() {
				return fmt.Errorf("the path given '%s' was not a directory", directory)
			}

			entrypoint := args[1]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("the entrypoint '%s' did not exists", entrypoint)
				}
				return fmt.Errorf("could not stat entrypoint '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("the path given '%s' was a directory", entrypoint)
			}

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return err
			}
			defer watcher.Close()

			waitCh, fn := onWatchChanges(ctx, watcher, device, sdk, entrypoint)
			go fn()

			if err := watcher.Add(directory); err != nil {
				return fmt.Errorf("failed to watch directory: '%s', reason: %w", directory, err)
			}

			<-waitCh
			return nil
		},
	}

	return cmd
}

func onWatchChanges(ctx context.Context, watcher *fsnotify.Watcher, device *Device, sdk *SDK, entrypoint string) (<-chan struct{}, func()) {
	doneCh := make(chan struct{})

	runOnDevice := func(runCtx context.Context) {
		fmt.Println("Compiling...")
		b, err := sdk.Build(runCtx, device, entrypoint)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		if err := device.Run(runCtx, b); err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Println("Successfully pushed program to device.")
	}

	runOnDevice(ctx)
	return doneCh, func() {
		defer close(doneCh)
		var previousCancel context.CancelFunc
		previousCancel = func() {}
		for {
			previousCancel()
			var innerCtx context.Context
			innerCtx, previousCancel = context.WithCancel(ctx)
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("File modified '%s'\n", event.Name)
					runOnDevice(innerCtx)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			case <-ctx.Done():
				return
			}
		}
	}
}
