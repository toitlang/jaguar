// Copyright (C) 2024 Toitware ApS. All rights reserved.
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
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/toitware/ubjson"
)

type DeviceNetwork struct {
	DeviceBase
	proxied bool
}

type SerializableDeviceNetwork struct {
	Kind       string `json:"kind" yaml:"kind" ubjson:"kind"`
	ID         string `json:"id" yaml:"id" ubjson:"id"`
	Name       string `json:"name" yaml:"name" ubjson:"name"`
	Chip       string `json:"chip" yaml:"chip" ubjson:"chip"`
	SDKVersion string `json:"sdkVersion" yaml:"sdkVersion" ubjson:"sdkVersion"`
	WordSize   int    `json:"wordSize" yaml:"wordSize" ubjson:"wordSize"`
	Address    string `json:"address" yaml:"address" ubjson:"address"`
	Proxied    bool   `json:"proxied" yaml:"proxied" ubjson:"proxied"`
}

func NewDeviceNetworkFromJson(data map[string]interface{}) (*DeviceNetwork, error) {
	var d DeviceNetwork
	// Print the data object for debugging:
	d.id = stringOr(data, "id", "")
	d.name = stringOr(data, "name", "")
	d.chip = stringOr(data, "chip", "esp32")
	d.sdkVersion = stringOr(data, "sdkVersion", "")
	d.wordSize = intOr(data, "wordSize", 4)
	d.address = stringOr(data, "address", "")
	d.proxied = boolOr(data, "proxied", false)
	return &d, nil
}

func NewDeviceNetworkFromSerializable(serializable *SerializableDeviceNetwork) (*DeviceNetwork, error) {
	return &DeviceNetwork{
		DeviceBase: DeviceBase{
			id:         serializable.ID,
			name:       serializable.Name,
			chip:       serializable.Chip,
			sdkVersion: serializable.SDKVersion,
			wordSize:   serializable.WordSize,
			address:    serializable.Address,
		},
		proxied: serializable.Proxied,
	}, nil
}

func (d *DeviceNetwork) ToSerializable() interface{} {
	return &SerializableDeviceNetwork{
		Kind:       "network",
		ID:         d.ID(),
		Name:       d.Name(),
		Chip:       d.Chip(),
		SDKVersion: d.SDKVersion(),
		WordSize:   d.WordSize(),
		Address:    d.Address(),
		Proxied:    d.proxied,
	}
}

func (d *DeviceNetwork) String() string {
	proxied := ""
	if d.proxied {
		proxied = ", proxied"
	}
	return fmt.Sprintf("%s (address: %s, %d-bit%s)", d.Name(), d.Address(), d.WordSize()*8, proxied)
}

const (
	pingTimeout = 3000 * time.Millisecond
)

func (d *DeviceNetwork) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
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

func (d *DeviceNetwork) Ping(ctx context.Context, sdk *SDK) bool {
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

func (d *DeviceNetwork) SendCode(ctx context.Context, sdk *SDK, request string, b []byte, headersMap map[string]string) error {
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

func (d *DeviceNetwork) ContainerList(ctx context.Context, sdk *SDK) (map[string]string, error) {
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

func (d *DeviceNetwork) ContainerUninstall(ctx context.Context, sdk *SDK, name string) error {
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

func (d *DeviceNetwork) UpdateFirmware(ctx context.Context, sdk *SDK, b []byte) error {
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

func ScanNetwork(ctx context.Context, ds deviceSelect, port uint) ([]Device, error) {
	// Check whether the ds is a deviceAddressSelect and if it is, use the address to identify the device.
	if _, ok := ds.(deviceAddressSelect); ok && ds.Address() != "" {
		addr := ds.Address()
		if !strings.Contains(addr, ":") {
			addr = addr + ":" + fmt.Sprint(scanHttpPort)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/identify", nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		buf, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("got non-OK from device: %s", res.Status)
		}
		dev, err := parseDeviceNetwork(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse identify. reason %w", err)
		} else if dev == nil {
			return nil, fmt.Errorf("invalid identify response")
		}
		return []Device{dev}, nil
	}

	pc, err := reuseport.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := pc.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	devices := map[string]Device{}
looping:
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if err == context.DeadlineExceeded {
				break looping
			}
			return nil, err
		default:
		}

		buf := make([]byte, 1024)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if isTimeoutError(err) {
				break looping
			}
			return nil, err
		}

		dev, err := parseDeviceNetwork(buf[:n])
		if err != nil {
			fmt.Println("Failed to parse identify", err)
		} else if dev != nil {
			devices[dev.Address()] = dev
		}
	}

	var res []Device
	for _, d := range devices {
		res = append(res, d)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name() < res[j].Name() })
	return res, nil
}

type udpMessage struct {
	Method  string                 `json:"method"`
	Payload map[string]interface{} `json:"payload"`
}

func parseDeviceNetwork(bytes []byte) (*DeviceNetwork, error) {
	var msg udpMessage
	if err := ubjson.Unmarshal(bytes, &msg); err != nil {
		if err := json.Unmarshal(bytes, &msg); err != nil {
			return nil, fmt.Errorf("could not parse message: %s. Reason: %w", string(bytes), err)

		}
	}

	if msg.Method != "jaguar.identify" {
		return nil, nil
	}

	device, err := NewDeviceNetworkFromJson(msg.Payload)
	if err != nil {
		return nil, fmt.Errorf("could not parse device: %w", err)
	}
	return device, nil
}

func isTimeoutError(err error) bool {
	e, ok := err.(net.Error)
	return ok && e.Timeout()
}
