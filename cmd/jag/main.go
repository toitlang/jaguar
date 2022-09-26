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
	date       = "2022-09-21T13:17:18Z"
	version    = "v1.6.6"
	sdkVersion = "v2.0.0-alpha.27"
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
	cmd := commands.JagCmd(info, isReleaseBuild)
	if err := cmd.ExecuteContext(ctx); err != nil {
		// The 'jag' command needs to have its "post run" function called
		// even when we exit with an error. The cobra framework doesn't
		// automatically call this, so we do it manually.
		cmd.PersistentPostRun(cmd, cmd.Flags().Args())
		os.Exit(1)
	}
}
