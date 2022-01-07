// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/viper"
)

const (
	JaguarDeviceIDHeader   = "X-Jaguar-Device-ID"
	JaguarSDKVersionHeader = "X-Jaguar-SDK-Version"
)

type Devices struct {
	Devices []Device `mapstructure:"devices" yaml:"devices" json:"devices"`
}

func (d Devices) Elements() []Short {
	var res []Short
	for _, d := range d.Devices {
		res = append(res, d)
	}
	return res
}

type Device struct {
	ID       string `mapstructure:"id" yaml:"id" json:"id"`
	Name     string `mapstructure:"name" yaml:"name" json:"name"`
	Address  string `mapstructure:"address" yaml:"address" json:"address"`
	WordSize int    `mapstructure:"wordSize" yaml:"wordSize" json:"wordSize"`
}

func (d Device) String() string {
	return fmt.Sprintf("%s (address: %s, %d-bit)", d.Name, d.Address, d.WordSize*8)
}

func (d Device) Short() string {
	return d.Name
}

const (
	pingTimeout = 400 * time.Millisecond
)

func (d Device) Ping(ctx context.Context, sdk *SDK) bool {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", d.Address+"/ping", nil)
	if err != nil {
		return false
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	return res.StatusCode == http.StatusOK
}

func (d Device) Run(ctx context.Context, sdk *SDK, b []byte) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+"/code", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non-OK from device: %s", res.Status)
	}

	return nil
}

func GetDevice(ctx context.Context, cfg *viper.Viper, sdk *SDK, checkPing bool, deviceSelect deviceSelect) (*Device, error) {
	manualPick := deviceSelect != nil
	if cfg.IsSet("device") && !manualPick {
		var d Device
		if err := cfg.UnmarshalKey("device", &d); err != nil {
			return nil, err
		}
		if checkPing {
			if d.Ping(ctx, sdk) {
				return &d, nil
			}
			deviceSelect = deviceIDSelect(d.ID)
			fmt.Printf("Failed to ping '%s'.\n", d.Name)
		} else {
			return &d, nil
		}
	}

	d, autoSelected, err := scanAndPickDevice(ctx, scanTimeout, scanPort, deviceSelect, manualPick)
	if err != nil {
		return nil, err
	}
	if !manualPick {
		if autoSelected {
			fmt.Printf("Found device '%s' again\n", d.Name)
		}
		cfg.Set("device", d)
		if err := cfg.WriteConfig(); err != nil {
			return nil, err
		}
	}
	return d, nil
}
