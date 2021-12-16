// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import "github.com/toitlang/jaguar/cmd/jag/commands"

var (
	date       = "2021-12-16T10:11:29Z"
	version    = "d40e01c"
	sdkVersion = "v0.10.4"
)

func main() {
	commands.JagCmd(commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}).Execute()
}
