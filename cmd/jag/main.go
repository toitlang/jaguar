// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"context"
	"os"

	"github.com/toitlang/jaguar/cmd/jag/commands"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

var (
	date       = "2022-09-17T05:42:38Z"
	version    = "v1.6.0"
	sdkVersion = "v2.0.0-alpha.25"
)

var buildMode = "development"

func main() {
	isReleaseBuild := buildMode == "release"
	directory.IsReleaseBuild = isReleaseBuild

	info := commands.Info{
		Date:       date,
		Version:    version,
		SDKVersion: sdkVersion,
	}
	ctx := commands.SetInfo(context.Background(), info)
	if err := commands.JagCmd(info, isReleaseBuild).ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
