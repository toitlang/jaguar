// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"context"
	"os"

	"github.com/toitlang/jaguar/cmd/jag/commands"
)

var (
	date       = "2022-01-31T13:14:30Z"
	version    = "v0.8.0"
	sdkVersion = "v0.16.0"
)

func main() {
	info := commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}
	ctx := commands.SetInfo(context.Background(), info)
	if err := commands.JagCmd(info).ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
