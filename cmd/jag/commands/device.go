// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/viper"
	"github.com/toitware/ubjson"
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

type DeviceNetwork struct {
	DeviceBase
	proxied bool
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

func NewDeviceNetworkFromJson(data map[string]interface{}) (*DeviceNetwork, error) {
	// Print the data object for debugging:
	return &DeviceNetwork{
		DeviceBase: DeviceBase{
			id:         stringOr(data, "id", ""),
			name:       stringOr(data, "name", ""),
			chip:       stringOr(data, "chip", "esp32"),
			sdkVersion: stringOr(data, "sdkVersion", ""),
			wordSize:   intOr(data, "wordSize", 4),
			address:    stringOr(data, "address", ""),
		},
		proxied: boolOr(data, "proxied", false),
	}, nil
}

func (d DeviceNetwork) String() string {
	proxied := ""
	if d.proxied {
		proxied = ", proxied"
	}
	return fmt.Sprintf("%s (address: %s, %d-bit%s)", d.Name(), d.Address(), d.WordSize()*8, proxied)
}

func (d DeviceNetwork) ToJson() map[string]interface{} {
	return map[string]interface{}{
		"id":         d.ID(),
		"name":       d.Name(),
		"chip":       d.Chip(),
		"sdkVersion": d.SDKVersion(),
		"wordSize":   d.WordSize(),
		"address":    d.Address(),
		"proxied":    d.proxied,
	}
}

const (
	pingTimeout = 3000 * time.Millisecond
)

func (d DeviceNetwork) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	lanIp, err := getLanIp()
	if err != nil {
		return nil, err
	}
	// If the device is on the same machine (proxied) use "localhost" instead of the
	// public IP. This is more stable on Windows machines.
	address := d.Address()
	if strings.HasPrefix(address, "http://"+lanIp+":") {
		address = "http://localhost:" + strings.TrimPrefix(address, "http://"+lanIp+":")
	}
	return http.NewRequestWithContext(ctx, method, address+path, body)
}

func (d DeviceNetwork) Ping(ctx context.Context, sdk *SDK) bool {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := d.newRequest(ctx, "GET", "/ping", nil)
	if err != nil {
		return false
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	return res.StatusCode == http.StatusOK
}

func (d DeviceNetwork) SendCode(ctx context.Context, sdk *SDK, request string, b []byte, headersMap map[string]string) error {
	req, err := d.newRequest(ctx, "PUT", request, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)
	for key, value := range headersMap {
		req.Header.Set(key, value)
	}
	// Set a crc32 header of the bytes.
	req.Header.Set(JaguarCRC32Header, fmt.Sprintf("%d", crc32.ChecksumIEEE(b)))

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

func (d DeviceNetwork) ContainerList(ctx context.Context, sdk *SDK) (map[string]string, error) {
	req, err := d.newRequest(ctx, "GET", "/list", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
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
	if err = ubjson.Unmarshal(body, &unmarshalled); err != nil {
		if err = json.Unmarshal(body, &unmarshalled); err != nil {
			return nil, err
		}
	}

	return unmarshalled, nil
}

func (d DeviceNetwork) ContainerUninstall(ctx context.Context, sdk *SDK, name string) error {
	req, err := d.newRequest(ctx, "PUT", "/uninstall", nil)
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
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

func (d DeviceNetwork) UpdateFirmware(ctx context.Context, sdk *SDK, b []byte) error {
	var reader = NewProgressReader(b)
	req, err := d.newRequest(ctx, "PUT", "/firmware", reader)
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(b))
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
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

func GetDevice(ctx context.Context, cfg *viper.Viper, sdk *SDK, checkPing bool, deviceSelect deviceSelect) (Device, error) {
	manualPick := deviceSelect != nil
	if cfg.IsSet("device") && !manualPick {
		var decoded map[string]interface{}
		if err := cfg.UnmarshalKey("device", &decoded); err != nil {
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
		cfg.Set("device", d.ToJson())
		if err := cfg.WriteConfig(); err != nil {
			return nil, err
		}
	}
	return d, nil
}
