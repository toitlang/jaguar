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
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"
)

type SDK struct {
	Path string
}

func GetSDK(ctx context.Context) (*SDK, error) {
	toit, ok := os.LookupEnv(directory.ToitPathEnv)
	if !ok {
		sdkCachePath, err := directory.GetSDKCachePath()
		if err != nil {
			return nil, err
		}
		if stat, err := os.Stat(sdkCachePath); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("you must setup the Toit SDK using 'jag setup'")
		}
		toit = sdkCachePath
	}

	res := &SDK{
		Path: toit,
	}
	return res, res.validate(ctx, ok)
}

func (s *SDK) ToitcPath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toitc"))
}

func (s *SDK) ToitvmPath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toitvm"))
}

func (s *SDK) ToitLspPath() string {
	return filepath.Join(s.Path, "bin", directory.Executable("toitlsp"))
}
func (s *SDK) VersionPath() string {
	return filepath.Join(s.Path, "VERSION")
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

func (s *SDK) Version() string {
	b, err := ioutil.ReadFile(s.VersionPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (s *SDK) validate(ctx context.Context, skipSDKVersionCheck bool) error {
	paths := []string{
		s.ToitcPath(),
		s.ToitvmPath(),
		s.ToitLspPath(),
		s.VersionPath(),
		s.SystemMessageSnapshotPath(),
		s.SnapshotToImagePath(),
		s.InjectConfigPath(),
	}
	for _, p := range paths {
		if stat, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("invalid Toit SDK, missing '%s'", p)
			}
			return fmt.Errorf("invalid Toit SDK, failed to load '%s', reason: %w", p, err)
		} else if stat.IsDir() {
			return fmt.Errorf("invalid Toit SDK, '%s' was a directory", p)
		}
	}

	if !skipSDKVersionCheck {
		info := GetInfo(ctx)
		if info.SDKVersion != s.Version() {
			return fmt.Errorf("invalid Toit SDK, %s is required, but found %s", info.SDKVersion, s.Version())
		}
	}

	return nil
}

func (s *SDK) Toitc(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitcPath(), args...)
}

func (s *SDK) Toitvm(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitvmPath(), args...)
}

func (s *SDK) ToitLsp(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitLspPath(), args...)
}

func (s *SDK) Build(ctx context.Context, device *Device, entrypoint string) ([]byte, error) {
	snapshotsCache, err := directory.GetSnapshotsCachePath()
	if err != nil {
		return nil, err
	}
	snapshot := filepath.Join(snapshotsCache, device.ID+".snapshot")

	buildSnap := s.Toitc(ctx, "-w", snapshot, entrypoint)
	buildSnap.Stderr = os.Stderr
	buildSnap.Stdout = os.Stdout
	if err := buildSnap.Run(); err != nil {
		return nil, err
	}

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

	buildImage := s.Toitvm(ctx, s.SnapshotToImagePath(), "--binary", bits, snapshot, image.Name())
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
