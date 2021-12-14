// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import "github.com/toitlang/jaguar/cmd/jag/commands"

var (
	date       = "2021-12-14T20:24:16Z"
	version    = "v0.1.1"
	sdkVersion = "v0.10.1"
)

func main() {
	commands.JagCmd(commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}).Execute()
}
