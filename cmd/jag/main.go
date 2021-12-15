// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import "github.com/toitlang/jaguar/cmd/jag/commands"

var (
	date       = "2021-12-15T08:55:23Z"
	version    = "v0.1.2"
	sdkVersion = "v0.10.2"
)

func main() {
	commands.JagCmd(commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}).Execute()
}
