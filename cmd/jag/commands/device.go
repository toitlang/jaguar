// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/toitlang/jaguar/cmd/jag/directory"
)

const (
	JaguarDeviceIDHeader          = "X-Jaguar-Device-ID"
	JaguarSDKVersionHeader        = "X-Jaguar-SDK-Version"
	JaguarWifiDisabledHeader      = "X-Jaguar-Wifi-Disabled"
	JaguarContainerNameHeader     = "X-Jaguar-Container-Name"
	JaguarContainerTimeoutHeader  = "X-Jaguar-Container-Timeout"
	JaguarContainerIntervalHeader = "X-Jaguar-Container-Interval"
	JaguarCRC32Header             = "X-Jaguar-CRC32"
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
	// Attempt to retrieve the value using the original key.
	val, ok := data[key]
	if !ok {
		// If not found, try the lowercase version of the key.
		val, ok = data[strings.ToLower(key)]
		if !ok {
			return def
		}
	}

	// Check if the value is an int or float64, and convert as needed.
	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return def
	}
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
