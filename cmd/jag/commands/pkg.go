// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/analytics"
	"github.com/toitlang/tpkg/commands"
	"github.com/toitlang/tpkg/config/store"
	"github.com/toitlang/tpkg/pkg/tracking"
	segment "gopkg.in/segmentio/analytics-go.v3"
)

func PkgCmd(info Info, analyticsClient analytics.Client) *cobra.Command {
	track := func(ctx context.Context, te *tracking.Event) error {
		properties := segment.Properties{
			"tpkg":   true,
			"jaguar": true,
		}
		for k, v := range te.Properties {
			properties[k] = v
		}
		analyticsClient.Enqueue(segment.Track{
			Event:      te.Name,
			Properties: properties,
		})
		return nil
	}

	s := store.NewViper("", info.SDKVersion, false, false)
	pkg, err := commands.Pkg(commands.DefaultRunWrapper, track, s, nil)
	if err != nil {
		panic(err)
	}
	return pkg
}
