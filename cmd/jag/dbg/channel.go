// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package dbg

// Channel is the transport seam between the relay engine and the target VM.
// The relay is written entirely against this interface and never touches pipes
// or sockets directly, so the host transport (a child VM's stdio pipes) and a
// future device transport (HTTP/UART) are interchangeable Channel impls.
type Channel interface {
	// Send writes one dbg: request line to the target (newline appended by the impl).
	Send(cmd string) error
	// Lines streams every line the target emits: dbg: responses interleaved with
	// the debugged program's own stdout. Closed when the target exits.
	Lines() <-chan string
	// Close detaches from the target and releases resources.
	Close() error
}
