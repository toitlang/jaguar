// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

//go:build !windows

package commands

import "syscall"

// checkPortWritable reports whether the given port can be opened for writing,
// without actually opening it.
//
// Opening (and closing) a serial port toggles the DTR/RTS modem-control lines,
// which resets boards that use the ESP32's integrated USB peripheral (for
// example the ESP32-S2) and takes them out of download mode. We therefore use
// access(2), which checks the calling process' permissions non-destructively.
func checkPortWritable(port string) error {
	const wOK = 0x2 // W_OK
	return syscall.Access(port, wOK)
}
