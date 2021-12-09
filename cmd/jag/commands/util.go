package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type SDK struct {
	Path string
}

func GetSDK() (*SDK, error) {
	toit, ok := os.LookupEnv(ToitPathEnv)
	if !ok {
		return nil, fmt.Errorf("You must set the env variable '%s'", ToitPathEnv)
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

func (s *SDK) validate() error {
	paths := []string{
		s.ToitcPath(),
		s.ToitvmPath(),
		s.SystemMessageSnapshotPath(),
		s.SnapshotToImagePath(),
	}
	for _, p := range paths {
		if stat, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("invalid toit SDK, missing '%s'", p)
			}
			return fmt.Errorf("invalid toit SDK, failed to load '%s', reason: %w", p, err)
		} else if stat.IsDir() {
			return fmt.Errorf("invalid toit SDK, '%s' was a directory", p)
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
