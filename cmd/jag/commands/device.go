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
)

const (
	JaguarDeviceIDHeader = "X-Jaguar-Device-ID"
)

type Device struct {
	ID       string `mapstructure:"id" yaml:"id" json:"id"`
	Name     string `mapstructure:"name" yaml:"name" json:"name"`
	Address  string `mapstructure:"address" yaml:"address" json:"address"`
	WordSize int    `mapstructure:"wordSize" yaml:"wordSize" json:"wordSize"`
}

func (d Device) String() string {
	return fmt.Sprintf("%s (address: %s, %d-bit)", d.Name, d.Address, d.WordSize*8)
}

const (
	pingTimeout = 400 * time.Millisecond
)

func (d Device) Ping(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", d.Address+"/ping", nil)
	if err != nil {
		return false
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	return res.StatusCode == http.StatusOK
}

func (d Device) Run(ctx context.Context, b []byte) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+"/code", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non OK from device: %d", res.StatusCode)
	}

	return nil
}
