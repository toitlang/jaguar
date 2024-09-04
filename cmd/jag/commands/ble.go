// Copyright (C) 2024 Florian Loitsch <florian@loitsch.com>
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"tinygo.org/x/bluetooth"
)

var globalAdapter = bluetooth.DefaultAdapter
var adapterEnabled = false

func EnabledAdapter() (*bluetooth.Adapter, error) {
	if !adapterEnabled {
		if err := globalAdapter.Enable(); err != nil {
			return nil, err
		}
		adapterEnabled = true
	}
	return globalAdapter, nil
}
