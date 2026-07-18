// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

//go:build windows

package commands

// checkPortWritable is a no-op on Windows. The caller already skips the port
// writability check there (the dialout/uucp group issue does not exist on
// Windows), so this exists only to satisfy the build on all platforms.
func checkPortWritable(port string) error {
	return nil
}
