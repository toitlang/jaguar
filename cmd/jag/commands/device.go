package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Device struct {
	Name    string `mapstructure:"name" yaml:"name" json:"name"`
	Address string `mapstructure:"address" yaml:"address" json:"address"`
}

func (d Device) String() string {
	return fmt.Sprintf("%s (address: %s)", d.Name, d.Address)
}

const (
	pingTimeout = 100 * time.Millisecond
)

func (d Device) Ping() bool {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", d.Address+"/ping", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	return res.StatusCode == http.StatusOK
}

func (d Device) Run(image io.Reader) error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "PUT", d.Address+"/code", image)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Got non OK from device: %d", res.StatusCode)
	}

	return nil
}