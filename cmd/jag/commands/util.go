package commands

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type SDK struct {
	Path string
}

func GetSDK() (*SDK, error) {
	toit, ok := os.LookupEnv(ToitPathEnv)
	if !ok {
		sdkCachePath, err := GetSDKCachePath()
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
	return res, res.validate()
}

func executable(str string) string {
	if runtime.GOOS == "windows" {
		return str + ".exe"
	}
	return str
}

func (s *SDK) ToitcPath() string {
	return filepath.Join(s.Path, "bin", executable("toitc"))
}

func (s *SDK) ToitvmPath() string {
	return filepath.Join(s.Path, "bin", executable("toitvm"))
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

func (s *SDK) validate() error {
	paths := []string{
		s.ToitcPath(),
		s.ToitvmPath(),
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
	return nil
}

func (s *SDK) Toitc(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitcPath(), args...)
}

func (s *SDK) Toitvm(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, s.ToitvmPath(), args...)
}

func (s *SDK) Build(ctx context.Context, device *Device, entrypoint string) ([]byte, error) {
	snapshotsCache, err := GetSnapshotsCachePath()
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
