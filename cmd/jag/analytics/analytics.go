// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package analytics

import (
	"time"

	"github.com/google/uuid"
	"github.com/toitlang/jaguar/cmd/jag/directory"
	"gopkg.in/segmentio/analytics-go.v3"
)

type Config struct {
	Disabled bool   `mapstructure:"disabled" yaml:"disabled" json:"disabled"`
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
	})
	if err != nil {
		return nil, err
	}

	return &proxyClient{
		disabled: res.Disabled,
		identity: &Identity{AnonymousID: res.ClientID},
		Client:   client,
	}, nil
}

type noopLogger struct{}

func (noopLogger) Logf(format string, args ...interface{})   {}
func (noopLogger) Errorf(format string, args ...interface{}) {}

type Client interface {
	analytics.Client
	Disable(bool)
}

type proxyClient struct {
	disabled bool
	analytics.Client
	identity *Identity
}

func (c *proxyClient) Disable(b bool) {
	c.disabled = b
}

func (c *proxyClient) Enqueue(msg analytics.Message) error {
	if c.disabled {
		return nil
	}

	return c.Client.Enqueue(c.identity.Populate(msg))
}

type Identity struct {
	userID      uuid.UUID
	AnonymousID string
}

func (i *Identity) UserID() string {
	if i.userID == uuid.Nil {
		return ""
	}
	return "user/" + i.userID.String()
}

func (i *Identity) Populate(msg analytics.Message) analytics.Message {
	switch t := msg.(type) {
	case analytics.Page:
		if t.UserId == "" {
			t.UserId = i.UserID()
		}
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	case analytics.Track:
		if t.UserId == "" {
			t.UserId = i.UserID()
		}
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	case analytics.Identify:
		if t.UserId == "" {
			t.UserId = i.UserID()
		}
		if t.AnonymousId == "" {
			t.AnonymousId = i.AnonymousID
		}
		return t
	default:
		return msg
	}
}
