// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"github.com/xtgo/uuid"
	"golang.org/x/term"
	"gopkg.in/yaml.v2"
)

type SDK struct {
	Path    string
	Version string
}

func GetSDK(ctx context.Context) (*SDK, error) {
	sdkPath, err := directory.GetSDKPath()
	if err != nil {
		return nil, err
	}

	version := ""
	toitPath := directory.GetToitPath(sdkPath)
	toitVersion := exec.CommandContext(ctx, toitPath, "version")
	versionBytes, err := toitVersion.Output()
	if err == nil {
		version = strings.TrimSpace(string(versionBytes))
	}

	res := &SDK{
		Path:    sdkPath,
		Version: version,
	}
	info := GetInfo(ctx)
	// If we're running a development build, we skip the SDK version checks
	// if the SDK is pulled in through the JAG_TOIT_REPO_PATH environment
	// variable. This make it much easier to work with. For release builds,
	// we always check and deliberately ignore the environment variable.
	skipVersionCheck := false
	if !directory.IsReleaseBuild {
		_, skipVersionCheck = os.LookupEnv(directory.ToitRepoPathEnv)
	}
	err = res.validate(info, skipVersionCheck)
	return res, err
}

func GetProgramAssetsPath(flags *pflag.FlagSet, flagName string) (string, error) {
	if !flags.Changed(flagName) {
		return "", nil
	}

	assetsPath, err := flags.GetString(flagName)
	if err != nil {
		return "", err
	}
	if stat, err := os.Stat(assetsPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no such file or directory: '%s'", assetsPath)
		}
		return "", fmt.Errorf("can't stat file '%s', reason: %w", assetsPath, err)
	} else if stat.IsDir() {
		return "", fmt.Errorf("can't use directory as assets: '%s'", assetsPath)
	}
	return assetsPath, nil
}

func (s *SDK) ToitPath() string {
	return directory.GetToitPath(s.Path)
}

func (s *SDK) DownloaderInfoPath() string {
	return filepath.Join(s.Path, "JAGUAR")
}

func (s *SDK) validate(info Info, skipSdkVersionCheck bool) error {
	if !skipSdkVersionCheck {
		if s.Version == "" {
			return fmt.Errorf("the SDK in '%s' is too old. Jaguar %s needs version %s.\nRun 'jag setup' to fix this", s.Path, info.Version, info.SDKVersion)
		} else if info.SDKVersion != s.Version {
			return fmt.Errorf("the SDK in '%s' is version %s, but Jaguar %s needs version %s.\nRun 'jag setup' to fix this", s.Path, s.Version, info.Version, info.SDKVersion)
		}

		downloaderInfoBytes, err := os.ReadFile(s.DownloaderInfoPath())
		if err != nil {
			return fmt.Errorf("the SDK in '%s' was not downloaded by Jaguar.\nRun 'jag setup' to fix this", s.Path)
		}

		downloaderInfo := Info{}
		err = json.Unmarshal(downloaderInfoBytes, &downloaderInfo)
		if err != nil {
			return fmt.Errorf("the SDK in '%s' was not downloaded by Jaguar.\nRun 'jag setup' to fix this", s.Path)
		}

		if downloaderInfo != info {
			return fmt.Errorf("the SDK in '%s' was not downloaded by this version of Jaguar.\nRun 'jag setup' to fix this", s.Path)
		}
	}

	paths := []string{
		s.ToitPath(),
	}
	for _, p := range paths {
		if err := checkFilepath(p, "invalid Toit SDK"); err != nil {
			return fmt.Errorf("%w.\nRun 'jag setup' to fix this", err)
		}
	}

	return nil
}

func checkFilepath(p string, invalidMsg string) error {
	if stat, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s, missing '%s'", invalidMsg, p)
		}
		return fmt.Errorf("%s, failed to load '%s', reason: %w", invalidMsg, p, err)
	} else if stat.IsDir() {
		return fmt.Errorf("%s, '%s' was a directory", invalidMsg, p)
	}
	return nil
}

func (s *SDK) ToitAnalyze(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"analyze"}, args...)...)
}

func (s *SDK) ToitCompile(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"compile"}, args...)...)
}

func (s *SDK) ToitRun(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"run", "--"}, args...)...)
}

func (s *SDK) ToitRunSnapshot(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), args...)
}

func (s *SDK) ToitLsp(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"tool", "lsp"}, args...)...)
}

func (s *SDK) AssetsTool(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"tool", "assets"}, args...)...)
}

func (s *SDK) FirmwareTool(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"tool", "firmware"}, args...)...)
}

type EspToolOutput struct {
	Command []string `json:"command"`
	Version string   `json:"version"`
}

func (s *SDK) EspToolCommand(ctx context.Context) ([]string, error) {
	args := []string{"--output-format", "json", "tool", "firmware", "tool", "esptool", "-e", "unused"}
	out, err := exec.CommandContext(ctx, s.ToitPath(), args...).Output()
	if err != nil {
		return nil, err
	}
	var result EspToolOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	command := result.Command
	return command, nil
}

func (s *SDK) EspTool(ctx context.Context, args ...string) (*exec.Cmd, error) {
	esptool_command, err := s.EspToolCommand(ctx)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, esptool_command[0], append(esptool_command[1:], args...)...), nil
}

func (s *SDK) SnapshotToImage(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"tool", "snapshot-to-image"}, args...)...)
}

func (s *SDK) SystemMessage(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"decode"}, args...)...)
}

func (s *SDK) Stacktrace(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), append([]string{"tool", "esp", "stacktrace"}, args...)...)
}

func (s *SDK) Compile(ctx context.Context, snapshot string, entrypoint string, optimizationLevel int) error {
	var buildSnap *exec.Cmd
	if optimizationLevel >= 0 {
		buildSnap = s.ToitCompile(ctx, "--snapshot", "-o", snapshot, "-O"+strconv.Itoa(optimizationLevel), entrypoint)
	} else {
		buildSnap = s.ToitCompile(ctx, "--snapshot", "-o", snapshot, entrypoint)
	}
	buildSnap.Stderr = os.Stderr
	buildSnap.Stdout = os.Stdout
	if err := buildSnap.Run(); err != nil {
		return err
	}
	return nil
}

func (s *SDK) Build(ctx context.Context, device Device, snapshotPath string, assetsPath string) ([]byte, error) {
	image, err := os.CreateTemp("", "*.image")
	if err != nil {
		return nil, err
	}
	image.Close()
	defer os.Remove(image.Name())

	bits := "-m32"
	if device.WordSize() == 8 {
		bits = "-m64"
	}

	arguments := []string{
		"--format=binary", bits,
		"--output", image.Name(),
		snapshotPath,
	}
	if assetsPath != "" {
		arguments = append(arguments, "--assets", assetsPath)
	}
	buildImage := s.SnapshotToImage(ctx, arguments...)
	buildImage.Stderr = os.Stderr
	buildImage.Stdout = os.Stdout
	if err := buildImage.Run(); err != nil {
		return nil, err
	}

	return os.ReadFile(image.Name())
}

func (s *SDK) PassThrough(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitPath(), args...)
}

type gzipReader struct {
	*gzip.Reader
	inner io.ReadCloser
}

func newGZipReader(r io.ReadCloser) (*gzipReader, error) {
	res, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &gzipReader{
		Reader: res,
		inner:  r,
	}, nil
}

func (r *gzipReader) Close() error {
	gzipErr := r.Reader.Close()
	rErr := r.inner.Close()
	if gzipErr != nil {
		return gzipErr
	}
	return rErr
}

func extractTarFile(fileReader io.Reader, destDir string, subDir string) error {
	dotSlashSubDir := "./" + subDir
	tarBallReader := tar.NewReader(fileReader)

	// Extract the input tar file
	for {
		header, err := tarBallReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		withoutPrefix := ""
		if strings.HasPrefix(header.Name, subDir) {
			withoutPrefix = strings.TrimPrefix(header.Name, subDir)
		} else if strings.HasPrefix(header.Name, dotSlashSubDir) {
			withoutPrefix = strings.TrimPrefix(header.Name, dotSlashSubDir)
		} else {
			continue
		}
		dirPath := filepath.Join(destDir, withoutPrefix)

		switch header.Typeflag {
		case tar.TypeDir:
			// handle directory
			err = os.MkdirAll(dirPath, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

		case tar.TypeReg:
			// handle files
			path := dirPath
			file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(file, tarBallReader); err != nil {
				file.Close()
				return err
			}

			if err := file.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

func ReadLine() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func ReadPassword() ([]byte, error) {
	return term.ReadPassword(int(syscall.Stdin))
}

type encoder interface {
	Encode(interface{}) error
}

func parseDefineFlags(cmd *cobra.Command, flagName string) (map[string]interface{}, error) {
	if !cmd.Flags().Changed(flagName) {
		return nil, nil
	}

	defineFlags, err := cmd.Flags().GetStringArray(flagName)
	if err != nil {
		return nil, err
	}

	definesMap := make(map[string]interface{})
	for _, element := range defineFlags {
		indexOfAssign := strings.Index(element, "=")
		var key string
		var value interface{}
		if indexOfAssign < 0 {
			key = strings.TrimSpace(element)
			value = true
		} else {
			key = strings.TrimSpace(element[0:indexOfAssign])
			valueString := strings.TrimSpace(element[indexOfAssign+1:])

			// Try to parse the value as a JSON value and avoid turning
			// it into a string if it is valid.
			var unmarshalled interface{}
			err := json.Unmarshal([]byte(valueString), &unmarshalled)
			if err == nil {
				value = unmarshalled
			} else {
				value = valueString
			}
		}
		if key == "jag.disabled" {
			fmt.Println("Warning: Using '-D jag.disabled' is deprecated. Use '-D jag.wifi=false' instead.")
			key = "jag.wifi"
			value = false
		}
		definesMap[key] = value
		if key == "run.boot" {
			fmt.Println()
			fmt.Println("*********************************************")
			fmt.Println("* Using 'jag run -D run.boot' is deprecated *")
			fmt.Println("* .. use 'jag container install' instead .. *")
			fmt.Println("*********************************************")
			fmt.Println()
		}
	}
	if len(definesMap) == 0 {
		return nil, nil
	}

	_, err = json.Marshal(definesMap)
	if err != nil {
		return nil, err
	}
	return definesMap, nil
}

func parseOutputFlag(cmd *cobra.Command) (encoder, error) {
	list, err := cmd.Flags().GetBool("list")
	if err != nil {
		return nil, err
	}
	if !list {
		return nil, nil
	}
	output, err := cmd.Flags().GetString("output")
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(output) {
	case "json":
		return json.NewEncoder(os.Stdout), nil
	case "yaml":
		return yaml.NewEncoder(os.Stdout), nil
	case "short":
		return newShortEncoder(os.Stdout), nil
	default:
		return nil, fmt.Errorf("--output flag '%s' was not recognized. Must be either json, yaml or short", output)
	}
}

func parseDeviceFlag(cmd *cobra.Command) (deviceSelect, error) {
	if !cmd.Flags().Changed("device") {
		return nil, nil
	}

	d, err := cmd.Flags().GetString("device")
	if err != nil {
		return nil, err
	}
	return parseDeviceSelection(d), nil
}

func parseDeviceSelection(d string) deviceSelect {
	if _, err := uuid.Parse(d); err == nil {
		return deviceIDSelect(d)
	}
	if strings.HasPrefix(d, "http://") {
		return deviceAddressSelect(d)
	}
	colonIdx := strings.Index(d, ":")
	if colonIdx > 0 {
		// Test if the part before the colon is a valid IP address.
		if ip := net.ParseIP(d[:colonIdx]); ip != nil {
			return deviceAddressSelect(d)
		}
	}
	if ip := net.ParseIP(d); ip != nil {
		return deviceAddressSelect(d)
	}
	return deviceNameSelect(d)
}

type shortEncoder struct {
	w io.Writer
}

func newShortEncoder(w io.Writer) *shortEncoder {
	return &shortEncoder{
		w: w,
	}
}

type Elements interface {
	Elements() []Short
}

type Short interface {
	Short() string
}

func (s *shortEncoder) Encode(v interface{}) error {
	es, ok := v.(Elements)
	if !ok {
		return fmt.Errorf("value type %T was not compatible with the Elements interface", v)
	}
	for _, e := range es.Elements() {
		fmt.Fprintln(s.w, e.Short())
	}
	return nil
}

func getWifiCredentials(cmd *cobra.Command) (string, string, error) {
	var wifiSSID string
	var err error

	cfg, err := directory.GetUserConfig()
	if err != nil {
		return "", "", err
	}

	if cmd.Flags().Changed("wifi-ssid") {
		wifiSSID, err = cmd.Flags().GetString("wifi-ssid")
		if err != nil {
			return "", "", err
		}
	} else if v, ok := os.LookupEnv(directory.WifiSSIDEnv); ok {
		wifiSSID = v
	} else if cfg.IsSet(WifiCfgKey + "." + WifiSSIDCfgKey) {
		wifiSSID = cfg.GetString(WifiCfgKey + "." + WifiSSIDCfgKey)
	} else {
		fmt.Printf("Enter WiFi network (SSID): ")
		wifiSSID, err = ReadLine()
		if err != nil {
			return "", "", err
		}
	}

	var wifiPassword string
	if cmd.Flags().Changed("wifi-password") {
		wifiPassword, err = cmd.Flags().GetString("wifi-password")
		if err != nil {
			return "", "", err
		}
	} else if v, ok := os.LookupEnv(directory.WifiPasswordEnv); ok {
		wifiPassword = v
	} else if cfg.IsSet(WifiCfgKey + "." + WifiPasswordCfgKey) {
		wifiPassword = cfg.GetString(WifiCfgKey + "." + WifiPasswordCfgKey)
	} else {
		fmt.Printf("Enter WiFi password for '%s': ", wifiSSID)
		pw := ""
		pwBytes, err := ReadPassword()
		if err == nil {
			pw = string(pwBytes)
			fmt.Printf("\n")
		} else {
			// Fall back to reading the password without hiding it.
			// On Windows git-bash, ReadPassword() might not work.
			pw, err = ReadLine()
			if err != nil {
				fmt.Printf("\n")
				return "", "", err
			}
		}
		wifiPassword = pw
	}
	return wifiSSID, wifiPassword, nil
}

func getUartEndpointOptions(cmd *cobra.Command) (map[string]interface{}, error) {
	uartEndpointRx, err := cmd.Flags().GetInt("uart-endpoint-rx")
	if err != nil {
		return nil, err
	}
	if uartEndpointRx < 0 {
		return nil, nil
	}
	uartEndpointOptions := map[string]interface{}{
		"rx": uartEndpointRx,
	}
	uartBaud, err := cmd.Flags().GetUint("uart-endpoint-baud")
	if err != nil {
		return nil, err
	}
	if uartBaud != 0 {
		uartEndpointOptions["baud"] = uartBaud
	}
	return uartEndpointOptions, nil
}

// isLikelyRunningOnBuildbot returns true if the current process is running on a buildbot.
// It uses some heuristics to determine this, and may not be 100% accurate.
func isLikelyRunningOnBuildbot() bool {
	envVars := []string{
		"JENKINS_HOME",
		"GITLAB_CI",
		"CI",
		"GITHUB_ACTIONS",
	}
	for _, envVar := range envVars {
		if _, ok := os.LookupEnv(envVar); ok {
			return true
		}
	}
	return false
}

func download(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed downloading from %s: %v", url, resp.Status)
	}

	if resp.Body == nil {
		return nil, fmt.Errorf("received nil response body from %s", url)
	}

	progress := NewProgressReader(resp.Body, resp.ContentLength)
	return progress, nil
}

// ProgressReader prints a progress bar while reading from an io.Reader.
type ProgressReader struct {
	reader      io.Reader
	size        int64
	current     int64
	spinState   int
	startTime   time.Time
	lastTime    time.Time
	lastCurrent int64
	speed       float64 // Bytes per second (EWMA)
}

func NewProgressReader(r io.Reader, size int64) *ProgressReader {
	return &ProgressReader{
		reader:    r,
		size:      size,
		startTime: time.Now(),
		lastTime:  time.Now(),
	}
}

func (p *ProgressReader) Read(buffer []byte) (n int, err error) {
	n, err = p.reader.Read(buffer)
	if n > 0 {
		p.current += int64(n)
		p.update()
	}
	return n, err
}

func (p *ProgressReader) Close() error {
	if closer, ok := p.reader.(io.ReadCloser); ok {
		return closer.Close()
	}
	return nil
}

func (p *ProgressReader) update() {
	now := time.Now()
	// Update every 100ms or if finished
	if p.current < p.size && now.Sub(p.lastTime) < 100*time.Millisecond {
		return
	}

	// EWMA Calculation for Speed
	// alpha = 0.9 (smoothing factor)
	alpha := 0.1
	instantSpeed := float64(p.current-p.lastCurrent) / now.Sub(p.lastTime).Seconds()
	if instantSpeed == 0 {
		// keep previous speed if stopped? or decay?
	} else if p.speed == 0 {
		p.speed = instantSpeed
	} else {
		p.speed = p.speed*(1-alpha) + instantSpeed*alpha
	}

	p.lastTime = now
	p.lastCurrent = p.current

	percent := int64(0)
	if p.size > 0 {
		percent = (p.current * 100) / p.size
	}
	fmt.Print("\r")
	// The strings must contain characters with the same UTF-8 length so that
	// they can be chopped up.  The emoji generally are 4-byte characters.
	// Braille are 3-byte characters, and of course ASCII is 1-byte characters.
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

	pos := int(percent) / (100 / parts)
	if pos >= parts {
		pos = parts - 1
	}
	p.spinState += spinBytesPerPart
	if p.spinState >= len(spin) {
		p.spinState = 0
	}
	spinChar := spin[p.spinState : p.spinState+spinBytesPerPart]
	fmt.Printf("   %3d%%  %s  [", percent, spinChar)
	fmt.Print(done[len(done)-(pos+1)*doneBytesPerPart:])
	fmt.Print(todo[:len(todo)-(pos+1)*todoBytesPerPart])
	fmt.Print("] ")

	// Speed display
	speedStr := fmt.Sprintf("%.1f MB/s", p.speed/1024/1024)
	if p.speed < 1024*1024 {
		speedStr = fmt.Sprintf("%.1f KB/s", p.speed/1024)
	}
	if p.speed < 1024 {
		speedStr = fmt.Sprintf("%.0f B/s", p.speed)
	}

	fmt.Printf("%s   ", speedStr)

	if p.current == p.size {
		fmt.Println()
	}
}

func getLanIp() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Println(err)
	}

	for _, addr := range addrs {
		if ipAddr, ok := addr.(*net.IPNet); ok && !ipAddr.IP.IsLoopback() {
			if ipAddr.IP.To4() != nil {
				return ipAddr.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no lan ip found")
}
