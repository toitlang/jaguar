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
	DeviceConfigPathEnv  = "JAG_DEVICE_CONFIG_PATH"
	SnapshotCachePathEnv = "JAG_SNAPSHOT_CACHE_PATH"
	configFile           = ".jaguar"

	ToitUserConfigPathEnv = "TOIT_CONFIG_FILE"

	// ToitPathEnv: Path to the Toit SDK build.
	ToitRepoPathEnv = "JAG_TOIT_REPO_PATH"
	// WifiSSIDEnv if set will use this wifi ssid.
	WifiSSIDEnv = "JAG_WIFI_SSID"
	// WifiPasswordEnv if set will use this wifi password.
	WifiPasswordEnv = "JAG_WIFI_PASSWORD"
)

// Hackishly set by main.go.
var IsReleaseBuild = false

func getConfigDirPath() (string, error) {
	if path, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		return path, nil
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homedir, ".config"), nil
}

func getStateDirPath() (string, error) {
	if path, ok := os.LookupEnv("XDG_STATE_HOME"); ok {
		return path, nil
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homedir, ".local", "state"), nil
}

func getCacheDirPath() (string, error) {
	if path, ok := os.LookupEnv("XDG_CACHE_HOME"); ok {
		return path, nil
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homedir, ".cache"), nil
}

func GetUserConfigPath() (string, error) {
	if path, ok := os.LookupEnv(UserConfigPathEnv); ok {
		return path, nil
	}

	configDir, err := getConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "jaguar", "config.yaml"), nil
}

func GetToitUserConfigPath() (string, error) {
	if path, ok := os.LookupEnv(ToitUserConfigPathEnv); ok {
		return path, nil
	}

	configDir, err := getConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "toit", "config.yaml"), nil
}

func GetDeviceConfigPath() (string, error) {
	if path, ok := os.LookupEnv(DeviceConfigPathEnv); ok {
		return path, nil
	}

	configDir, err := getConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "jaguar", "device.yaml"), nil
}

func GetSnapshotsPaths() ([]string, error) {
	paths := []string{}
	path, ok := os.LookupEnv(SnapshotCachePathEnv)
	if ok {
		paths = append(paths, path)
	}

	stateDir, err := getStateDirPath()
	if err != nil {
		return nil, err
	}

	cacheDir, err := getCacheDirPath()
	if err != nil {
		return nil, err
	}

	// We are not using the XDG here, as we used to write snapshots
	// directly in to the home/.cache/jaguar/snapshots directory, ignoring
	// the XDG environment variables.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	paths = append(paths, filepath.Join(stateDir, "toit", "snapshots"))
	cachePath := filepath.Join(cacheDir, "jaguar", "snapshots")
	homeCachePath := filepath.Join(home, ".cache", "jaguar", "snapshots")
	paths = append(paths, cachePath)
	if cachePath != homeCachePath {
		// For backwards compatibility add the home cache path as well.
		paths = append(paths, homeCachePath)
	}
	return paths, nil
}

func GetSnapshotsStatePath() (string, error) {
	path, ok := os.LookupEnv(SnapshotCachePathEnv)
	if ok {
		return path, nil
	}

	stateDir, err := getStateDirPath()
	if err != nil {
		return "", err
	}

	return ensureDirectory(filepath.Join(stateDir, "toit", "snapshots"), nil)
}

func GetRepoPath() (string, bool) {
	if IsReleaseBuild {
		return "", false
	}
	return os.LookupEnv(ToitRepoPathEnv)
}

func GetSDKPath(jagVersion string) (string, error) {
	repoPath, ok := GetRepoPath()
	if ok {
		return filepath.Join(repoPath, "build", "host", "sdk"), nil
	}
	sdkCachePath, err := GetSDKCachePath(jagVersion)
	if err != nil {
		return "", err
	}
	if stat, err := os.Stat(sdkCachePath); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("the path '%s' did not hold the SDK.\nYou must setup the SDK using 'jag setup'", sdkCachePath)
	}
	return sdkCachePath, nil
}

func GetToitPath(sdkPath string) string {
	return filepath.Join(sdkPath, "bin", Executable("toit"))
}

func GetJagCachePath(jagVersion string) (string, error) {
	cachePath, err := getCacheDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(cachePath, "jaguar", jagVersion), nil
}

func GetSDKCachePath(jagVersion string) (string, error) {
	jagCachePath, err := GetJagCachePath(jagVersion)
	if err != nil {
		return "", err
	}
	return filepath.Join(jagCachePath, "sdk"), nil
}

func GetEnvelopesCachePath(jagVersion string) (string, error) {
	jagCachePath, err := GetJagCachePath(jagVersion)
	if err != nil {
		return "", err
	}
	return filepath.Join(jagCachePath, "envelopes"), nil
}

func GetAssetsCachePath(jagVersion string) (string, error) {
	jagCachePath, err := GetJagCachePath(jagVersion)
	if err != nil {
		return "", err
	}
	return filepath.Join(jagCachePath, "assets"), nil
}

func GetAssetsPath(jagVersion string) (string, error) {
	_, ok := GetRepoPath()
	if ok {
		// We assume that the jag executable is inside the build directory of
		// the Jaguar repository.
		execPath, err := os.Executable()
		if err != nil {
			return "", err
		}
		dir := filepath.Dir(execPath)
		return filepath.Join(dir, "assets"), nil
	}

	assetsPath, err := GetAssetsCachePath(jagVersion)
	if err != nil {
		return "", err
	}
	if stat, err := os.Stat(assetsPath); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("the path '%s' does not hold the Jaguar assets.\nYou must setup the assets using 'jag setup'", assetsPath)
	}
	return assetsPath, nil
}

func GetJaguarSnapshotPath(jagVersion string) (string, error) {
	assetsPath, err := GetAssetsPath(jagVersion)
	if err != nil {
		return "", nil
	}

	name := "jaguar.snapshot"
	path := filepath.Join(assetsPath, name)
	if stat, err := os.Stat(path); err != nil || stat.IsDir() {
		return "", fmt.Errorf("the path '%s' does not hold the asset '%s'.\nYou must setup the Jaguar assets using 'jag setup'", assetsPath, name)
	}
	return path, nil
}

func ensureDirectory(dir string, err error) (string, error) {
	if err != nil {
		return dir, err
	}
	return dir, os.MkdirAll(dir, 0755)
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

func GetDeviceConfig() (*viper.Viper, error) {
	path, err := GetDeviceConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get device config path: %w", err)
	}

	cfg := viper.New()
	cfg.SetConfigType("yaml")
	cfg.SetConfigFile(path)
	if _, err := os.Stat(path); err == nil {
		if err := cfg.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read device config: %w", err)
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
