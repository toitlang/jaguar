// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

const (
	JaguarDeviceIDHeader         = "X-Jaguar-Device-ID"
	JaguarSDKVersionHeader       = "X-Jaguar-SDK-Version"
	JaguarDisabledHeader         = "X-Jaguar-Disabled"
	JaguarContainerNameHeader    = "X-Jaguar-Container-Name"
	JaguarContainerTimeoutHeader = "X-Jaguar-Container-Timeout"
	JaguarCRC32Header            = "X-Jaguar-CRC32"
)

type Device interface {
	ID() string
	Name() string
	Chip() string
	SDKVersion() string
	WordSize() int
	Address() string
	Short() string
	String() string

	SetID(string)
	SetSDKVersion(string)

	Ping(ctx context.Context, sdk *SDK) bool
	SendCode(ctx context.Context, sdk *SDK, request string, b []byte, headersMap map[string]string) error
	ContainerList(ctx context.Context, sdk *SDK) (map[string]string, error)
	ContainerUninstall(ctx context.Context, sdk *SDK, name string) error
	UpdateFirmware(ctx context.Context, sdk *SDK, b []byte) error

	ToJson() map[string]interface{}
}

type Devices struct {
	Devices []Device
}

func (d Devices) Elements() []Short {
	var res []Short
	for _, d := range d.Devices {
		res = append(res, d)
	}
	return res
}

type DeviceBase struct {
	id         string
	name       string
	chip       string
	sdkVersion string
	wordSize   int
	address    string
}

func (d DeviceBase) ID() string {
	return d.id
}

func (d DeviceBase) Name() string {
	return d.name
}

func (d DeviceBase) Chip() string {
	return d.chip
}

func (d DeviceBase) SDKVersion() string {
	return d.sdkVersion
}

func (d DeviceBase) WordSize() int {
	return d.wordSize
}

func (d DeviceBase) Address() string {
	return d.address
}

func (d DeviceBase) SetID(id string) {
	d.id = id
}

func (d DeviceBase) SetSDKVersion(version string) {
	d.sdkVersion = version
}

func (d DeviceBase) Short() string {
	return d.Name()
}

func (d DeviceBase) String() string {
	return fmt.Sprintf("%s (address: %s, %d-bit)", d.Name(), d.Address(), d.WordSize()*8)
}

func NewDeviceFromJson(data map[string]interface{}) (Device, error) {
	return NewDeviceNetworkFromJson(data)
}
func boolOr(data map[string]interface{}, key string, def bool) bool {
	if val, ok := data[key].(bool); ok {
		return val
	}
	// Viper converts all keys to lowercase, so we need to check for that as well.
	key = strings.ToLower(key)
	if val, ok := data[key].(bool); ok {
		return val
	}
	return def
}

func stringOr(data map[string]interface{}, key string, def string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	// Viper converts all keys to lowercase, so we need to check for that as well.
	key = strings.ToLower(key)
	if val, ok := data[key].(string); ok {
		return val
	}
	return def
}

func intOr(data map[string]interface{}, key string, def int) int {
	if val, ok := data[key].(float64); ok {
		return int(val)
	}
	// Viper converts all keys to lowercase, so we need to check for that as well.
	key = strings.ToLower(key)
	if val, ok := data[key].(float64); ok {
		return int(val)
	}
	return def
}

func GetDevice(ctx context.Context, sdk *SDK, checkPing bool, deviceSelect deviceSelect) (Device, error) {
	deviceCfg, err := directory.GetDeviceConfig()
	if err != nil {
		return nil, err
	}
	manualPick := deviceSelect != nil
	if deviceCfg.IsSet("device") && !manualPick {
		var decoded map[string]interface{}
		if err := deviceCfg.UnmarshalKey("device", &decoded); err != nil {
			return nil, err
		}
		d, err := NewDeviceFromJson(decoded)
		if err != nil {
			return nil, err
		}
		if checkPing {
			if d.Ping(ctx, sdk) {
				return d, nil
			}
			deviceSelect = deviceIDSelect(d.ID())
			fmt.Printf("Failed to ping '%s'.\n", d.Name())
		} else {
			return d, nil
		}
	}

	d, autoSelected, err := scanAndPickDevice(ctx, scanTimeout, scanPort, deviceSelect, manualPick)
	if err != nil {
		return nil, err
	}
	if !manualPick {
		if autoSelected {
			fmt.Printf("Found device '%s' again\n", d.Name())
		}
		deviceCfg.Set("device", d.ToJson())
		if err := deviceCfg.WriteConfig(); err != nil {
			return nil, err
		}
	}
	return d, nil
}
