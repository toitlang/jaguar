// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"github.com/toitlang/tpkg/commands"
	"github.com/toitlang/tpkg/config/store"
	"github.com/toitlang/tpkg/pkg/tracking"
)

func PkgCmd(info Info) *cobra.Command {
	track := func(ctx context.Context, te *tracking.Event) error {
		// We've already handled the necessary Jaguar analytics, so we take
		// care to ignore any additional attempts to track usage.
		return nil
	}

	configStore := store.NewViper("", info.SDKVersion, false, false)
	cobra.OnInitialize(func() {
		cfgFile, _ := directory.GetToitUserConfigPath()
		configStore.Init(cfgFile)
	})

	pkg, err := commands.Pkg(commands.DefaultRunWrapper, track, configStore, nil)
	if err != nil {
		panic(err)
	}
	return pkg
}
