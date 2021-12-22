// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"context"

	"github.com/toitlang/jaguar/cmd/jag/commands"
)

var (
	date       = "2021-12-22T20:28:57Z"
	version    = "v0.4.1"
	sdkVersion = "v0.11.1"
)

func main() {
	info := commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}
	ctx := commands.SetInfo(context.Background(), info)
	commands.JagCmd(info).ExecuteContext(ctx)
}
