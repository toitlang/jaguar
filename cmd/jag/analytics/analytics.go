// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package analytics

import (
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"gopkg.in/segmentio/analytics-go.v3"
)

type Config struct {
	Disabled bool   `mapstructure:"disabled" yaml:"disabled" json:"disabled"`
	First    bool   `mapstructure:"first" yaml:"first" json:"first"`
	ClientID string `mapstructure:"cid" yaml:"cid" json:"cid"`
}

const (
	writeKey = "VMZlvLv5U9C4Pp5cMpuISq5k8dcN0gL2"
)

func GetClient() (Client, error) {
	cfg, err := directory.GetUserConfig()
	if err != nil {
		return nil, err
	}

	var res Config
	rewrite := true
	if cfg.IsSet("analytics") {
		if err := cfg.UnmarshalKey("analytics", &res); err == nil {
			rewrite = false
		} else {
			rewrite = true
		}
		if res.ClientID == "" {
			rewrite = true
		}
	}

	if rewrite {
		res.ClientID = uuid.New().String()
		res.First = true
		cfg.Set("analytics", res)
		if err := directory.WriteConfig(cfg); err != nil {
			return nil, err
		}
	}

	client, err := analytics.NewWithConfig(writeKey, analytics.Config{
		Interval:  time.Millisecond,
		BatchSize: 1,
		Endpoint:  "https://segmentapi.toit.io",
		Logger:    noopLogger{},
		Callback: callback{
			Config:     &res,
			UserConfig: cfg,
		},
	})
	if err != nil {
		return nil, err
	}

	return &proxyClient{
		Client:   client,
		identity: &Identity{AnonymousID: res.ClientID},
		config:   &res,
	}, nil
}

type callback struct {
	Config     *Config
	UserConfig *viper.Viper
}

func (c callback) Success(message analytics.Message) {
	if !c.Config.First {
		return
	}
	c.Config.First = false
	c.UserConfig.Set("analytics", c.Config)
	// If the call to WriteConfig fails, we might send
	// multiple messages with the first flag set to true,
	// but that is something we can live with.
	directory.WriteConfig(c.UserConfig)
}

func (c callback) Failure(message analytics.Message, err error) {
	// Do nothing.
}

type noopLogger struct{}

func (noopLogger) Logf(format string, args ...interface{})   {}
func (noopLogger) Errorf(format string, args ...interface{}) {}

type Client interface {
	analytics.Client
	First() bool
}

type proxyClient struct {
	analytics.Client
	identity *Identity
	config   *Config
}

func (c *proxyClient) First() bool {
	return c.config.First
}

func (c *proxyClient) Enqueue(msg analytics.Message) error {
	if c.config.Disabled {
		return nil
	}

	return c.Client.Enqueue(c.identity.Populate(msg))
}

type Identity struct {
	AnonymousID string
}

func (i *Identity) Populate(msg analytics.Message) analytics.Message {
	switch t := msg.(type) {
	case analytics.Page:
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	case analytics.Track:
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	case analytics.Identify:
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	default:
		return msg
	}
}
