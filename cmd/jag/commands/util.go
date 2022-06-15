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
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"github.com/xtgo/uuid"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"
)

type SDK struct {
	Path    string
	Version string
}

func GetSDK(ctx context.Context) (*SDK, error) {
	toit, err := directory.GetSDKPath()
	if err != nil {
		return nil, err
	}

	versionPath := filepath.Join(toit, "VERSION")

	version := ""
	versionBytes, err := ioutil.ReadFile(versionPath)
	if err == nil {
		version = strings.TrimSpace(string(versionBytes))
	}

	res := &SDK{
		Path:    toit,
		Version: version,
	}
	info := GetInfo(ctx)
	_, skipVersionCheck := os.LookupEnv(directory.ToitRepoPathEnv)
	return res, res.validate(info, skipVersionCheck)
}

func (s *SDK) ToitCompilePath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toit.compile"))
}

func (s *SDK) ToitRunPath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toit.run"))
}

func (s *SDK) ToitRunSnapshotPath() string {
	return filepath.Join(s.Path, "bin", "toit.run.snapshot")
}

func (s *SDK) ToitLspPath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toit.lsp"))
}

func (s *SDK) VersionPath() string {
	return filepath.Join(s.Path, "VERSION")
}

func (s *SDK) DownloaderInfoPath() string {
	return filepath.Join(s.Path, "JAGUAR")
}

func (s *SDK) SystemMessageSnapshotPath() string {
	return filepath.Join(s.Path, "snapshots", "system_message.snapshot")
}

func (s *SDK) SnapshotToImagePath() string {
	return filepath.Join(s.Path, "snapshots", "snapshot_to_image.snapshot")
}

func (s *SDK) InjectConfigPath() string {
	return filepath.Join(s.Path, "snapshots", "inject_config.snapshot")
}

func (s *SDK) validate(info Info, skipSDKVersionCheck bool) error {
	if !skipSDKVersionCheck {
		if s.Version == "" {
			return fmt.Errorf("SDK in '%s' is too old. Jaguar %s needs version %s.\nRun 'jag setup' to fix this.", s.Path, info.Version, info.SDKVersion)
		} else if info.SDKVersion != s.Version {
			return fmt.Errorf("SDK in '%s' is version %s, but Jaguar %s needs version %s.\nRun 'jag setup' to fix this.", s.Path, s.Version, info.Version, info.SDKVersion)
		}

		downloaderInfoBytes, err := ioutil.ReadFile(s.DownloaderInfoPath())
		if err != nil {
			return fmt.Errorf("SDK in '%s' was not downloaded by Jaguar.\nRun 'jag setup' to fix this.", s.Path)
		}

		downloaderInfo := Info{}
		err = json.Unmarshal(downloaderInfoBytes, &downloaderInfo)
		if err != nil {
			return fmt.Errorf("SDK in '%s' was not downloaded by Jaguar.\nRun 'jag setup' to fix this.", s.Path)
		}

		if downloaderInfo != info {
			return fmt.Errorf("SDK in '%s' was not downloaded by this version of Jaguar.\nRun 'jag setup' to fix this.", s.Path)
		}
	}

	paths := []string{
		s.ToitCompilePath(),
		s.ToitRunPath(),
		s.ToitRunSnapshotPath(),
		s.ToitLspPath(),
		s.VersionPath(),
		s.SystemMessageSnapshotPath(),
		s.SnapshotToImagePath(),
		s.InjectConfigPath(),
	}
	for _, p := range paths {
		if err := checkFilepath(p, "invalid Toit SDK"); err != nil {
			return fmt.Errorf("%w.\nRun 'jag setup' to fix this.", err)
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

func (s *SDK) ToitCompile(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitCompilePath(), args...)
}

func (s *SDK) ToitRun(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitRunPath(), args...)
}

func (s *SDK) ToitLsp(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitLspPath(), args...)
}

func (s *SDK) Compile(ctx context.Context, snapshot string, entrypoint string) error {
	buildSnap := s.ToitCompile(ctx, "-w", snapshot, entrypoint)
	buildSnap.Stderr = os.Stderr
	buildSnap.Stdout = os.Stdout
	if err := buildSnap.Run(); err != nil {
		return err
	}
	return nil
}

func (s *SDK) Build(ctx context.Context, device *Device, snapshot string) ([]byte, error) {
	image, err := os.CreateTemp("", "*.image")
	if err != nil {
		return nil, err
	}
	image.Close()
	defer os.Remove(image.Name())

	bits := "-m32"
	if device.WordSize == 8 {
		bits = "-m64"
	}

	buildImage := s.ToitRun(ctx, s.SnapshotToImagePath(), "--binary", bits, "--output", image.Name(), snapshot)
	buildImage.Stderr = os.Stderr
	buildImage.Stdout = os.Stdout
	if err := buildImage.Run(); err != nil {
		return nil, err
	}

	return ioutil.ReadFile(image.Name())
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

		if !strings.HasPrefix(header.Name, subDir) {
			continue
		}

		dirPath := filepath.Join(destDir, strings.TrimPrefix(header.Name, subDir))

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
	fd := int(os.Stdin.Fd())
	oldState, err := terminal.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer func() {
		terminal.Restore(fd, oldState)
		fmt.Printf("******\n")
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer signal.Stop(c)
	defer func() { close(c) }()
	go func() {
		select {
		case _, ok := <-c:
			if ok {
				terminal.Restore(fd, oldState)
				fmt.Printf("\n")
				os.Exit(1)
			}
		}
	}()

	return terminal.ReadPassword(fd)
}

type encoder interface {
	Encode(interface{}) error
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
		return nil, fmt.Errorf("--ouput flag '%s' was not recognized. Must be either json, yaml or short.", output)
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
	if _, err := uuid.Parse(d); err == nil {
		return deviceIDSelect(d), nil
	}
	return deviceNameSelect(d), nil
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
