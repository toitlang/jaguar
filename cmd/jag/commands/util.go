package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type Cmd struct {
	path string
}

func (c *Cmd) Cmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, c.path, args...)
}

func Toitc() (*Cmd, error) {
	toitc, ok := os.LookupEnv(ToitcPathEnv)
	if !ok {
		return nil, fmt.Errorf("You must set the env variable '%s'", ToitcPathEnv)
	}

	return &Cmd{path: toitc}, nil
}

func Toitvm() (*Cmd, error) {
	toitvm, ok := os.LookupEnv(ToitvmPathEnv)
	if !ok {
		return nil, fmt.Errorf("You must set the env variable '%s'", ToitvmPathEnv)
	}

	return &Cmd{path: toitvm}, nil
}
