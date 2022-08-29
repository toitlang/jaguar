// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/analytics"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	segment "gopkg.in/segmentio/analytics-go.v3"
)

type ctxKey string

const (
	ctxKeyInfo ctxKey = "info"
)

type Info struct {
	Version    string `mapstructure:"version" yaml:"version" json:"version"`
	Date       string `mapstructure:"date" yaml:"date" json:"date"`
	SDKVersion string `mapstructure:"sdkVersion" yaml:"sdkVersion" json:"sdkVersion"`
}

func SetInfo(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, ctxKeyInfo, info)
}

func GetInfo(ctx context.Context) Info {
	return ctx.Value(ctxKeyInfo).(Info)
}

func JagCmd(info Info, isReleaseBuild bool) *cobra.Command {
	var analyticsClient analytics.Client
	configCmd := ConfigCmd(info)

	cmd := &cobra.Command{
		Use:   "jag",
		Short: "Fast development for your ESP32",
		Long: "Jaguar is a Toit application for your ESP32 that gives you the fastest development cycle.\n\n" +
			"Jaguar uses the capabilities of the Toit virtual machine to let you update and restart your\n" +
			"ESP32 applications written in Toit over WiFi. Change your Toit code in your editor, update\n" +
			"the application on your device, and restart it all within seconds. No need to flash over\n" +
			"serial, reboot your device, or wait for it to reconnect to your network.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Avoid running the analytics and up-to-date check code when
			// the command is a subcommand of 'config'.
			current := cmd
			for current.HasParent() {
				if current == configCmd {
					return
				}
				current = current.Parent()
			}

			// Be careful and assign to the outer analyticsClient, so
			// we can close it correctly in the post-run action.
			var err error
			analyticsClient, err = analytics.GetClient()
			if err != nil {
				return
			}

			properties := segment.Properties{
				"jaguar":   true,
				"command":  (*cobra.Command)(cmd).UseLine(),
				"platform": runtime.GOOS,
			}

			if isReleaseBuild {
				properties.Set("version", info.Version)
			} else {
				properties.Set("version", "development")
			}

			go analyticsClient.Enqueue(segment.Page{
				Name:       "CLI Execute",
				Properties: properties,
			})

			CheckUpToDate(info)
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if analyticsClient != nil {
				analyticsClient.Close()
			}
		},
	}

	cmd.AddCommand(
		ScanCmd(),
		PingCmd(),
		RunCmd(),
		CompileCmd(),
		SimulateCmd(),
		DecodeCmd(),
		SetupCmd(info),
		FlashCmd(),
		FirmwareCmd(),
		MonitorCmd(),
		WatchCmd(),
		PortCmd(),
		ToitCmd(),
		PkgCmd(info),
		configCmd,
		VersionCmd(info, isReleaseBuild),
	)
	return cmd
}

type UpdateToDate struct {
	Disabled    bool   `mapstructure:"disabled" yaml:"disabled" json:"disabled"`
	LastSuccess string `mapstructure:"lastSuccess" yaml:"lastSuccess" json:"lastSuccess"`
	LastAttempt string `mapstructure:"lastAttempt" yaml:"lastAttempt" json:"lastAttempt"`
}

const UpToDateKey = "up-to-date"

func CheckUpToDate(info Info) {
	if !directory.IsReleaseBuild {
		return
	}

	// Only run the update checks when we're outputting to a TTY.
	stat, err := os.Stdout.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) == 0 {
		return
	}

	cfg, err := directory.GetUserConfig()
	if err != nil {
		return
	}

	var res UpdateToDate
	rewrite := true
	if cfg.IsSet(UpToDateKey) {
		if err := cfg.UnmarshalKey(UpToDateKey, &res); err == nil {
			rewrite = false
		}
	}

	if rewrite {
		res.Disabled = false
		res.LastSuccess = ""
		res.LastAttempt = ""
	} else if res.Disabled {
		return
	}

	now := time.Now()
	if res.LastAttempt != "" {
		if last, err := time.Parse(time.RFC3339, res.LastAttempt); err == nil {
			elapsed := now.Sub(last)
			// We don't want to use the network or ask GitHub too often, so we only
			// attempt once every 5 minutes.
			if elapsed < 5*time.Minute {
				// Skip check. It is too soon to even try again.
				return
			}
		}
	}

	if res.LastSuccess != "" {
		if last, err := time.Parse(time.RFC3339, res.LastSuccess); err == nil {
			elapsed := now.Sub(last)
			if elapsed < 24*7*time.Hour {
				// Skip check. We successfully checked not long ago.
				return
			}
		} else {
			res.LastSuccess = ""
		}
	}

	res.LastAttempt = now.Format(time.RFC3339)
	cfg.Set(UpToDateKey, res)
	if err := directory.WriteConfig(cfg); err != nil {
		return
	}

	// Construct the URL we're fetching version information from.
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		"toitlang/jaguar",
		"releases/latest")

	// Create an HTTP request with the bare minimum headers.
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	request.Header.Add("User-Agent", "jaguar-cli")

	client := http.Client{}
	response, err := client.Do(request)
	if err != nil || response.StatusCode < 200 || response.StatusCode > 299 {
		return
	}

	bodyText, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return
	}

	result := make(map[string]interface{})
	json.Unmarshal(bodyText, &result)

	tagNameBytes, err := json.Marshal(result["tag_name"])
	if err != nil {
		return
	}

	tagName := string(tagNameBytes)
	matched, err := regexp.MatchString(`^\s*"?\s*v\d+\.\d+\.\d+\s*"?\s*$`, tagName)
	if err != nil || !matched {
		return
	}

	tagName = strings.TrimSpace(tagName)
	tagName = strings.TrimPrefix(tagName, "\"")
	tagName = strings.TrimSuffix(tagName, "\"")
	currentVersion := semver.New(info.Version[1:])
	latestVersion := semver.New(strings.TrimSpace(tagName)[1:])

	if currentVersion.LessThan(*latestVersion) {
		banner := strings.Repeat("-", 60)
		fmt.Println()
		fmt.Println(banner)
		fmt.Println("There is a newer version of Jaguar available (v" + latestVersion.String() + "). You may")
		fmt.Println("want to update using your package manager or download the new")
		fmt.Println("version directly from:")
		fmt.Println()
		fmt.Println("  https://github.com/toitlang/jaguar/releases/latest")
		fmt.Println()
		fmt.Println("You can disable these periodic up-to-date checks through:")
		fmt.Println()
		fmt.Println("  $ jag config up-to-date disable")
		fmt.Println()
		fmt.Println("Have a great day.")
		fmt.Println(banner)
		fmt.Println()
	}

	res.LastSuccess = res.LastAttempt
	cfg.Set(UpToDateKey, res)
	if err := directory.WriteConfig(cfg); err != nil {
		fmt.Println(err)
		return
	}
}
