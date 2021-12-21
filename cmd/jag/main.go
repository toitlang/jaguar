// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"context"

	"github.com/toitlang/jaguar/cmd/jag/commands"
)

var (
	date       = "2021-12-20T20:58:49Z"
	version    = "v0.3.2"
	sdkVersion = "v0.10.5"
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
