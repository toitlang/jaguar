// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

func WatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "watch <file>",
		Short:        "Watch for changes to <file> and its dependencies and automatically re-run the code",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := directory.GetDeviceConfig()
			if err != nil {
				return err
			}

			programAssetsPath, err := GetProgramAssetsPath(cmd.Flags(), "assets")
			if err != nil {
				return err
			}

			entrypoint := args[0]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't watch directory: '%s'", entrypoint)
			}

			ctx := cmd.Context()
			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}

			device, err := GetDevice(ctx, cfg, sdk, true, deviceSelect)
			if err != nil {
				return err
			}

			watcher, err := newWatcher()
			if err != nil {
				return err
			}
			defer watcher.Close()

			waitCh, fn := onWatchChanges(cmd, watcher, device, sdk, entrypoint, programAssetsPath)
			go fn()

			<-waitCh
			return nil
		},
	}
	cmd.Flags().StringP("device", "d", "", "use device with a given name, id, or address")
	cmd.Flags().String("assets", "", "attach assets to the program")
	return cmd
}

type watcher struct {
	sync.Mutex
	watcher *fsnotify.Watcher

	paths map[string]struct{}
}

func newWatcher() (*watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &watcher{
		watcher: w,
		paths:   map[string]struct{}{},
	}, nil
}

func (w *watcher) Close() error {
	return w.watcher.Close()
}

func (w *watcher) Events() chan fsnotify.Event {
	return w.watcher.Events
}

func (w *watcher) Errors() chan error {
	return w.watcher.Errors
}

func (w *watcher) CountPaths() int {
	return len(w.paths)
}

func (w *watcher) Watch(paths ...string) (err error) {
	for i, p := range paths {
		if paths[i], err = filepath.EvalSymlinks(p); err != nil {
			return err
		}
	}

	candidates := map[string]struct{}{}
	for _, p := range paths {
		if _, ok := w.paths[p]; !ok {
			w.Mutex.Lock()
			w.watcher.Add(p)
			w.paths[p] = struct{}{}
			w.Mutex.Unlock()
		}
		candidates[p] = struct{}{}
	}

	for p := range w.paths {
		if _, ok := candidates[p]; !ok {
			w.Mutex.Lock()
			w.watcher.Remove(p)
			delete(w.paths, p)
			w.Mutex.Unlock()
		}
	}
	return nil
}

func parseDependeniesToDirs(b []byte) []string {
	m := map[string]struct{}{}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		p := strings.TrimSuffix(strings.TrimSpace(scanner.Text()), ":")
		if _, err := os.Stat(p); err == nil {
			m[p] = struct{}{}
		}
	}
	var res []string
	for r := range m {
		res = append(res, r)
	}
	return res
}

func onWatchChanges(
	cmd *cobra.Command,
	watcher *watcher,
	device *Device,
	sdk *SDK,
	entrypoint string,
	assetsPath string) (<-chan struct{}, func()) {
	doneCh := make(chan struct{})
	ctx := cmd.Context()

	updateWatcher := func(runCtx context.Context) {
		var paths []string
		if tmpFile, err := ioutil.TempFile("", "*.txt"); err == nil {
			defer os.Remove(tmpFile.Name())
			tmpFile.Close()
			cmd := sdk.ToitCompile(ctx, "--dependency-file", tmpFile.Name(), "--dependency-format", "plain", "--analyze", entrypoint)
			if err := cmd.Run(); err == nil {
				if b, err := ioutil.ReadFile(tmpFile.Name()); err == nil {
					paths = parseDependeniesToDirs(b)
				}
			} else {
				// A compilation error happened, we let the watch paths be if there was some.
				if watcher.CountPaths() > 0 {
					return
				}
			}
		}

		if len(paths) == 0 {
			paths = []string{filepath.Dir(entrypoint)}
		}

		if err := watcher.Watch(paths...); err != nil {
			fmt.Println("Failed to update watcher: ", err)
		}
	}

	runOnDevice := func(runCtx context.Context) {
		if err := RunFile(cmd, device, sdk, entrypoint, nil, assetsPath); err != nil {
			fmt.Println("Error:", err)
			return
		}
	}

	firstCtx, previousCancel := context.WithCancel(ctx)
	go updateWatcher(firstCtx)
	runOnDevice(firstCtx)
	return doneCh, func() {
		defer close(doneCh)
		fired := false
		ticketDuration := 100 * time.Millisecond
		ticker := time.NewTicker(ticketDuration)
		defer ticker.Stop()
		for {
			select {
			case event, ok := <-watcher.Events():
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					if !fired {
						fmt.Printf("File modified '%s'\n", event.Name)
						previousCancel()
						var innerCtx context.Context
						innerCtx, previousCancel = context.WithCancel(ctx)
						go updateWatcher(innerCtx)
						go runOnDevice(innerCtx)
						fired = true
						ticker.Reset(ticketDuration)
					}
				}
			case <-ticker.C:
				fired = false
			case err, ok := <-watcher.Errors():
				if !ok {
					return
				}
				fmt.Println("Watch error:", err)
			case <-ctx.Done():
				return
			}
		}
	}
}
