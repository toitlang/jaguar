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
	date       = "2022-02-10T16:35:08Z"
	version    = "v1.0.3"
	sdkVersion = "v1.6.6"
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
