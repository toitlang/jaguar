// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/spf13/viper"
)

const (
	JaguarDeviceIDHeader      = "X-Jaguar-Device-ID"
	JaguarSDKVersionHeader    = "X-Jaguar-SDK-Version"
	JaguarDefinesHeader       = "X-Jaguar-Defines"
	JaguarContainerNameHeader = "X-Jaguar-Container-Name"
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
	ID         string `mapstructure:"id" yaml:"id" json:"id"`
	Name       string `mapstructure:"name" yaml:"name" json:"name"`
	Address    string `mapstructure:"address" yaml:"address" json:"address"`
	SDKVersion string `mapstructure:"sdkVersion" yaml:"sdkVersion" json:"sdkVersion"`
	WordSize   int    `mapstructure:"wordSize" yaml:"wordSize" json:"wordSize"`
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

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	return res.StatusCode == http.StatusOK
}

func (d Device) SendCode(ctx context.Context, sdk *SDK, request string, b []byte, name string, defines string) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+request, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	if defines != "" {
		req.Header.Set(JaguarDefinesHeader, defines)
	}
	if name != "" {
		req.Header.Set(JaguarContainerNameHeader, name)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non-OK from device: %s", res.Status)
	}

	return nil
}

func (d Device) ContainerList(ctx context.Context, sdk *SDK) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", d.Address+"/list", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got non-OK from device: %s", res.Status)
	}

	var unmarshalled map[string]string
	if err = json.Unmarshal(body, &unmarshalled); err != nil {
		return nil, err
	}

	return unmarshalled, nil
}

func (d Device) ContainerUninstall(ctx context.Context, sdk *SDK, name string) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+"/uninstall", nil)
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	req.Header.Set(JaguarContainerNameHeader, name)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non-OK from device: %s", res.Status)
	}
	return nil
}

// A Reader based on a byte array that prints a progress bar.
type ProgressReader struct {
	b         []byte
	index     int
	spinState int
}

func NewProgressReader(b []byte) *ProgressReader {
	return &ProgressReader{b, 0, 0}
}

func (p *ProgressReader) Read(buffer []byte) (n int, err error) {
	if p.index == len(p.b) {
		return 0, io.EOF
	}
	copied := copy(buffer, p.b[p.index:])
	p.index += copied
	percent := (p.index * 100) / len(p.b)
	fmt.Print("\r")
	// The strings must contain characters with the same UTF-8 length so that
	// they can be chopped up.  The emoji generally are 4-byte characters.
	// Braille are 3-byte characters, and or course ASCII is 1-byte characters.
	spin := "⠁⠂⠄⡀⢀⠠⠐⠈"
	done := "🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱🐱"
	todo := "--------------------------------------------------"
	if os.PathSeparator == '\\' { // Windows.
		spin = "/-\\|"
		done = "################### Jaguar #######################"
	}

	parts := utf8.RuneCountInString(done)
	todoParts := utf8.RuneCountInString(todo)
	if todoParts < parts {
		parts = todoParts
	}
	spinStates := utf8.RuneCountInString(spin)
	doneBytesPerPart := len(done) / parts
	todoBytesPerPart := len(todo) / parts
	spinBytesPerPart := len(spin) / spinStates

	pos := percent / (100 / parts)
	p.spinState += spinBytesPerPart
	if p.spinState == len(spin) {
		p.spinState = 0
	}
	spinChar := spin[p.spinState : p.spinState+spinBytesPerPart]
	fmt.Printf("   %3d%%  %4dk  %s  [", percent, p.index>>10, spinChar)
	fmt.Print(done[len(done)-pos*doneBytesPerPart:])
	fmt.Print(todo[:len(todo)-pos*todoBytesPerPart])
	fmt.Print("] ")
	return copied, nil
}

func (d Device) UpdateFirmware(ctx context.Context, sdk *SDK, b []byte) error {
	var reader = NewProgressReader(b)
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+"/firmware", reader)
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(b))
	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	defer fmt.Print("\n\n")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
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
