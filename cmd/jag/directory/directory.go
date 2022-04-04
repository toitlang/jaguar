// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package directory

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

const (
	// UserConfigPathEnv if set, will load the user config from that path.
	UserConfigPathEnv    = "JAG_USER_CONFIG_PATH"
	WorkspacePathEnv     = "JAG_WORKSPACE_PATH"
	SnapshotCachePathEnv = "JAG_SNAPSHOT_CACHE_PATH"
	configFile           = ".jaguar"

	// ToitPathEnv: Path to the Toit SDK build.
	ToitPathEnv = "JAG_TOIT_PATH"
	// EsptoolPathEnv: Path to the esptool.
	EsptoolPathEnv = "JAG_ESPTOOL_PATH"
	// ImageEnv: Path to the Jaguar pre-built image for the ESP32.
	Esp32ImageEnv = "JAG_ESP32_IMAGE_PATH"
	// WifiSSIDEnv if set will use this wifi ssid.
	WifiSSIDEnv = "JAG_WIFI_SSID"
	// WifiPasswordEnv if set will use this wifi password.
	WifiPasswordEnv = "JAG_WIFI_PASSWORD"
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

func GetWorkspaceConfigPath() (string, error) {
	ws, err := GetWorkspacePath()
	return filepath.Join(ws, configFile), err
}

func GetUserConfigPath() (string, error) {
	if path, ok := os.LookupEnv(UserConfigPathEnv); ok {
		return path, nil
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homedir, ".config", "jaguar", "config.yaml"), nil
}

func GetSnapshotsCachePath() (string, error) {
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

func GetESP32ImageCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "jaguar", "image"), nil
}

func GetESP32ImagePath() (string, error) {
	imagePath, ok := os.LookupEnv(Esp32ImageEnv)
	if ok {
		return imagePath, nil
	}

	imagePath, err := GetESP32ImageCachePath()
	if err != nil {
		return "", err
	}
	if stat, err := os.Stat(imagePath); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("the path '%s' did not hold the esp32 image.\nYou must setup the esp32 image using 'jag setup'", imagePath)
	}
	return imagePath, nil
}

func GetJaguarSnapshotPath() (string, error) {
	imagePath, err := GetESP32ImagePath()
	if err != nil {
		return "", err
	}
	snapshotPath := filepath.Join(imagePath, "jaguar.snapshot")

	if stat, err := os.Stat(snapshotPath); err != nil || stat.IsDir() {
		return "", fmt.Errorf("the path '%s' did not hold the snapshot file.\nYou must setup the Jaguar snapshot using 'jag setup'", snapshotPath)
	}
	return snapshotPath, nil
}

func GetEsptoolCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "jaguar", Executable("esptool")), nil
}

func GetEsptoolPath() (string, error) {
	esptoolPath, ok := os.LookupEnv(EsptoolPathEnv)
	if ok {
		return esptoolPath, nil
	}

	cachePath, err := GetEsptoolCachePath()
	if err != nil {
		return "", err
	}

	if stat, err := os.Stat(cachePath); err != nil || stat.IsDir() {
		return "", fmt.Errorf("the path '%s' did not hold the esptool.\nYou must setup the esptool using 'jag setup'", cachePath)
	}
	return cachePath, nil
}

func ensureDirectory(dir string, err error) (string, error) {
	if err != nil {
		return dir, err
	}
	return dir, os.MkdirAll(dir, 0755)
}

func GetWorkspaceConfig() (*viper.Viper, error) {
	path, err := GetWorkspaceConfigPath()
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to get workspace config path: %w", err)
	}
	cfg := viper.New()
	cfg.SetConfigType("yaml")
	cfg.SetConfigFile(path)
	if !os.IsNotExist(err) {
		if err := cfg.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read workspace config: %w", err)
		}
	}
	return cfg, nil
}

func GetUserConfig() (*viper.Viper, error) {
	path, err := GetUserConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get user config path: %w", err)
	}

	cfg := viper.New()
	cfg.SetConfigType("yaml")
	cfg.SetConfigFile(path)
	if _, err := os.Stat(path); err == nil {
		if err := cfg.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read user config: %w", err)
		}
	}
	return cfg, nil
}

func WriteConfig(cfg *viper.Viper) error {
	file := cfg.ConfigFileUsed()
	dir := filepath.Dir(file)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	tmpFile := filepath.Join(filepath.Dir(file), ".config.tmp.yaml")
	if err := cfg.WriteConfigAs(tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	return os.Rename(tmpFile, file)
}

func Executable(str string) string {
	if runtime.GOOS == "windows" {
		return str + ".exe"
	}
	return str
}
