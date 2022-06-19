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
	date       = "2022-06-19T05:40:43Z"
	version    = "v1.2.4"
	sdkVersion = "v2.0.0-alpha.7"
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
