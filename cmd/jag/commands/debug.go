// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/dbg"
)

// DebugCmd implements `jag debug [-d host] <file.toit> [--script <cmds>]`: an
// interactive bytecode debugger for programs run on the host VM.
func DebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug <file>",
		Short: "Debug Toit code on the host VM",
		Long: "Compile <file> to a snapshot and run it under the Toit VM debugger.\n" +
			"Provides an interactive 'dbg>' REPL (gdb-style: b/c/s/n/f/i/m), or a\n" +
			"scripted run with --script for CI. Only '-d host' is supported.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			deviceSelect, err := parseDeviceFlag(cmd)
			if err != nil {
				return err
			}
			// Default to host; reject any explicit non-host target.
			if deviceSelect != nil {
				name, ok := deviceSelect.(deviceNameSelect)
				if !ok || string(name) != "host" {
					return fmt.Errorf("device debugging is not yet supported (only -d host)")
				}
			}

			entrypoint := args[0]
			if stat, err := os.Stat(entrypoint); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no such file or directory: '%s'", entrypoint)
				}
				return fmt.Errorf("can't stat file '%s', reason: %w", entrypoint, err)
			} else if stat.IsDir() {
				return fmt.Errorf("can't debug directory: '%s'", entrypoint)
			}

			scriptPath, err := cmd.Flags().GetString("script")
			if err != nil {
				return err
			}

			sdk, err := GetSDK(ctx)
			if err != nil {
				return err
			}
			return runDebug(ctx, sdk, entrypoint, scriptPath)
		},
	}
	cmd.Flags().StringP("device", "d", "", "device to debug (only 'host' is supported)")
	cmd.Flags().String("script", "", "read debugger commands from a file instead of the interactive REPL")
	return cmd
}

// runDebug compiles entrypoint to a snapshot, builds the offline name map,
// launches the VM in debug mode, and runs the relay (REPL or scripted).
func runDebug(ctx context.Context, sdk *SDK, entrypoint, scriptPath string) error {
	// 1. Compile to a snapshot in a temp dir (ephemeral; the debugger does not
	//    need the snapshot registered in jag's cache the way `run`/`decode` do).
	tmpdir, err := os.MkdirTemp("", "jag_debug")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)
	snapshot := filepath.Join(tmpdir, "prog.snapshot")
	if err := sdk.Compile(ctx, snapshot, entrypoint, -1); err != nil {
		return err // compiler diagnostics already went to stderr
	}

	// 2. Offline name map.
	bytecodes, err := sdk.SnapshotBytecodes(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("failed to read snapshot bytecodes: %w", err)
	}
	names := dbg.ParseBytecodes(string(bytecodes))

	// 3. Launch the VM in debug mode and wrap its pipes in a Channel.
	runCmd := sdk.ToitRunDebug(ctx, snapshot)
	channel, err := newStdioChannel(runCmd)
	if err != nil {
		return fmt.Errorf("failed to start debug VM (is this a debug-capable SDK?): %w", err)
	}

	// 4. Relay.
	session := dbg.NewSession(channel, names, os.Stdout)
	if err := session.Start(); err != nil {
		channel.Close()
		return fmt.Errorf("VM did not become ready: %w", err)
	}
	if err := session.Methods(); err != nil {
		channel.Close()
		return fmt.Errorf("failed to fetch method registry: %w", err)
	}

	if scriptPath != "" {
		runScript(session, scriptPath)
	} else {
		runREPL(session)
	}

	// Detach and report the VM's exit status.
	return channel.Close()
}

func runREPL(session *dbg.Session) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("dbg> ")
	for scanner.Scan() {
		stop, err := session.Do(scanner.Text())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if stop {
			return
		}
		fmt.Print("dbg> ")
	}
}

func runScript(session *dbg.Session, path string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open script '%s': %v\n", path, err)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		stop, err := session.Do(scanner.Text())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if stop {
			return
		}
	}
}

// stdioChannel is the host dbg.Channel: it wraps a child VM's stdin/stdout
// pipes. The only concrete transport in this design.
type stdioChannel struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
}

func newStdioChannel(cmd *exec.Cmd) (*stdioChannel, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	ch := &stdioChannel{cmd: cmd, stdin: stdin, lines: make(chan string, 256)}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // tolerate long lines
		for scanner.Scan() {
			ch.lines <- scanner.Text()
		}
		close(ch.lines)
	}()
	return ch, nil
}

func (c *stdioChannel) Send(cmd string) error {
	_, err := io.WriteString(c.stdin, strings.TrimRight(cmd, "\n")+"\n")
	return err
}

func (c *stdioChannel) Lines() <-chan string { return c.lines }

func (c *stdioChannel) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}
