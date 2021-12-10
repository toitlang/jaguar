// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	WorkspacePathEnv     = "JAG_WORKSPACE_PATH"
	SnapshotCachePathEnv = "JAG_SNAPSHOT_CACHE_PATH"
	configFile           = ".jaguar"

	// EntryPointEnv snapshot of the Jaguar program.
	EntryPointEnv = "JAG_ENTRY_POINT"
	// ToitPathEnv path to the Toit sdk build.
	ToitPathEnv = "JAG_TOIT_PATH"
	// EsptoolPathEnv path to the esptool.
	EsptoolPathEnv = "JAG_ESPTOOL_PATH"
	// ESP32BinEnv path to the jaguar esp32 binary image.
	ESP32BinEnv = "JAG_ESP32_BIN_PATH"
)

func GetWorkspacePath() (string, error) {
	path, ok := os.LookupEnv(WorkspacePathEnv)
	if ok {
		return path, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd

	for {
		candidate := filepath.Join(dir, configFile)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return dir, nil
		}

		next := filepath.Dir(dir)
		if next == dir {
			return cwd, os.ErrNotExist
		}
		dir = next
	}
}

func GetConfigPath() (string, error) {
	ws, err := GetWorkspacePath()
	return filepath.Join(ws, configFile), err
}

func GetSnapshotCachePath() (string, error) {
	path, ok := os.LookupEnv(SnapshotCachePathEnv)
	if ok {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return ensureDirectory(filepath.Join(home, ".cache", "jaguar", "snapshots"), nil)
}

func GetSDKCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "jaguar", "sdk"), nil
}

func GetImageCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "jaguar", "image"), nil
}

func ensureDirectory(dir string, err error) (string, error) {
	if err != nil {
		return dir, err
	}
	return dir, os.MkdirAll(dir, 0755)
}

func GetConfig() (*viper.Viper, error) {
	path, err := GetConfigPath()
	if err != nil && err != os.ErrNotExist {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}
	cfg := viper.New()
	cfg.SetConfigType("yaml")
	cfg.SetConfigFile(path)
	if err != os.ErrNotExist {
		if err := cfg.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}
	return cfg, nil
}

func GetDevice(ctx context.Context, cfg *viper.Viper, checkPing bool) (*Device, error) {
	if cfg.IsSet("device") {
		var d Device
		if err := cfg.UnmarshalKey("device", &d); err != nil {
			return nil, err
		}
		if checkPing {
			if d.Ping(ctx) {
				return &d, nil
			}
			fmt.Println("Couldn't ping the device, select a new device")
		} else {
			return &d, nil
		}
	}

	d, err := scanAndPickDevice(ctx, scanTimeout, scanPort)
	if err != nil {
		return nil, err
	}
	cfg.Set("device", d)
	if err := cfg.WriteConfig(); err != nil {
		return nil, err
	}
	return d, nil
}
