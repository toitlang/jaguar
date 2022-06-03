// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"golang.org/x/net/websocket"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmank88/ubjson"
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

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
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

	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non-OK from device: %s", res.Status)
	}

	return nil
}

func (d Device) Log(ctx context.Context, sdk *SDK, snapshotsCache string) error {
	req, err := websocket.NewConfig(d.Address+"/log", d.Address+"/log")
	if err != nil {
		return err
	}

	req.Header.Set(JaguarDeviceIDHeader, d.ID)
	req.Header.Set(JaguarSDKVersionHeader, sdk.Version)

	req.Location.Scheme = "ws"

	ws, err := websocket.DialConfig(req)
	if err != nil {
		return err
	}

	var msg = make([]byte, 512*1024)
	for {
		n, err := ws.Read(msg)
		if err != nil {
			return err
		}

		err = d.outputLog(ctx, sdk, snapshotsCache, msg[:n])
		if err != nil {
			fmt.Printf("Failed to output received log: %s\n", err)
		}
	}
}

var levels = []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

func (d Device) outputLog(ctx context.Context, sdk *SDK, snapshotsCache string, msg []byte) error {
	var logs []interface{}
	err := ubjson.Unmarshal(msg, &logs)
	if err != nil {
		return err
	}

	arrLevel := logs[0]
	arrMsg := logs[1]
	arrNames := logs[2]
	arrKeys := logs[3]
	arrValues := logs[4]
	arrTrace := logs[5]

	level, ok := arrLevel.(uint8)
	if !ok {
		return fmt.Errorf("failed to decode log level")
	}

	names, err := asStringArray(arrNames)
	if err != nil {
		return err
	}

	keys, err := asArray(arrKeys)
	if err != nil {
		return err
	}

	values, err := asArray(arrValues)
	if err != nil {
		return err
	}

	var keyValues = ""
	if len(keys) > 0 && len(values) > 0 && len(keys) == len(values) {
		sep := ""
		for i := 0; i < len(keys); i++ {
			keyValues += fmt.Sprintf("%s%s: %s", sep, keys[i], values[i])
			sep = ", "
		}

		keyValues = fmt.Sprintf(" {%s}", keyValues)
	}

	fmt.Printf("[%s] %s: %s%s\n", strings.Join(names, "."), levels[level], arrMsg, keyValues)

	if arrTrace != nil {
		snapshot := filepath.Join(snapshotsCache, d.ID+".snapshot")
		encoded := base64.StdEncoding.EncodeToString(arrTrace.([]byte))
		decodeCmd := sdk.ToitRun(ctx, sdk.SystemMessageSnapshotPath(), snapshot, "-b", encoded)
		decodeCmd.Stderr = os.Stderr
		decodeCmd.Stdout = os.Stdout
		err = decodeCmd.Run()
		if err != nil {
			fmt.Printf("Failed to decode snapshot: %s\n", err)
		}
	}
	return nil
}

func asArray(arr interface{}) ([]interface{}, error) {
	res, ok := arr.([]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to decode log")
	}
	return res, nil
}

func asStringArray(arrNames interface{}) ([]string, error) {
	var names []string
	if arrNames != nil {
		names_, ok := arrNames.([]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to decode log")
		}
		for _, val := range names_ {
			val_, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("failed to decode log")
			}
			names = append(names, val_)
		}
	}
	return names, nil
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
