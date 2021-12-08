package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	ConfigPathEnv         = "JAG_CONFIG_PATH"
	configFile            = ".shaguar"
	ToitvmPathEnv         = "TOITVM_PATH"
	ToitcPathEnv          = "TOITC_PATH"
	ToitSnap2ImagePathEnv = "TOIT_SNAP_TO_IMAGE_PATH"
)

func GetConfigPath() (string, error) {
	path, ok := os.LookupEnv(ConfigPathEnv)
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
			return candidate, nil
		}

		next := filepath.Dir(dir)
		if next == dir {
			return filepath.Join(cwd, configFile), os.ErrNotExist
		}
		dir = next
	}
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
			if d.Ping() {
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
