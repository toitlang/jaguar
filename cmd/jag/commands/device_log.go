// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/toitware/ubjson"
)

// LogEntry is one captured line of device output, as returned by GET /log. On
// the wire each entry is a fixed-order array [seq, container, type, level, text]
// (the device omits the field names to roughly halve the encoded size); see
// logEntryFromArray for the decoding.
type LogEntry struct {
	Seq       int
	Container string
	Type      string
	// Level can be nullable, in case of print/trace/exit entries.
	Level *int
	Text  string
}

// logEntryFromArray decodes the device's positional [seq, container, type,
// level, text] array into a LogEntry. Numbers arrive as int64 (ubjson) or
// float64 (json), so the conversions accept both.
func logEntryFromArray(raw interface{}) (LogEntry, error) {
	// Number of elements in the wire array form of a LogEntry.
	const logEntryArity = 5

	arr, ok := raw.([]interface{})
	if !ok || len(arr) != logEntryArity {
		return LogEntry{}, fmt.Errorf("malformed log entry, expected %d-element array: %v", logEntryArity, raw)
	}

	var err error
	entry := LogEntry{}
	if entry.Seq, err = decodeInt(arr, 0); err != nil {
		return entry, err
	}
	if entry.Container, err = decodeString(arr, 1); err != nil {
		return entry, err
	}
	if entry.Type, err = decodeString(arr, 2); err != nil {
		return entry, err
	}
	// Level can be null, in case of print/trace/exit.
	if arr[3] != nil {
		level, err := decodeInt(arr, 3)
		if err != nil {
			return entry, err
		}
		entry.Level = &level
	}
	if entry.Text, err = decodeString(arr, 4); err != nil {
		return entry, err
	}

	return entry, nil
}

// decodeInt reads element i as an int. Numbers arrive as int64 (ubjson) or float64 (json).
func decodeInt(arr []interface{}, i int) (int, error) {
	switch n := arr[i].(type) {
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("log entry field %d: expected number, got %T", i, arr[i])
	}
}

func decodeString(arr []interface{}, i int) (string, error) {
	s, ok := arr[i].(string)
	if !ok {
		return "", fmt.Errorf("log entry field %d: expected string, got %T", i, arr[i])
	}
	return s, nil
}

// LogPollResponse is the decoded body of a GET /log response.
type LogPollResponse struct {
	// Next is the cursor to pass to the following poll. The device caps each
	// response, so Next may be short of Head; polling again with it pages through
	// the backlog.
	Next    int
	Head    int
	Oldest  int
	Entries []LogEntry
}

// logPollWire mirrors the GET /log body before the entries are turned into
// LogEntry values: entries arrive as positional arrays (see LogEntry), so they
// are decoded generically here and converted by logEntryFromArray.
type logPollWire struct {
	Next    int           `json:"next"`
	Head    int           `json:"head"`
	Oldest  int           `json:"oldest"`
	Entries []interface{} `json:"entries"`
}

// PollLog drains GET /log, filtered to the given container names (the device
// stamps each entry with its producing container: empty for `/run` programs,
// the install name for `/install`-ed containers). An empty slice means no
// filter; the empty string matches the anonymous `/run` programs.
func (d DeviceNetwork) PollLog(ctx context.Context, cursor int, containers []string) (*LogPollResponse, error) {
	// Repeat the "containers" parameter once per name. An empty slice sends none
	// (no filter); an empty-string entry matches the anonymous `/run` programs,
	// which is exactly what `run -m` wants.
	var pathBuilder strings.Builder
	fmt.Fprintf(&pathBuilder, "/log?cursor=%d", cursor)
	for _, container := range containers {
		fmt.Fprintf(&pathBuilder, "&containers=%s", url.QueryEscape(container))
	}
	req, err := d.newRequest(ctx, "GET", pathBuilder.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
	// The device skips the SDK-version check for /log. Its handler negotiates on
	// Accept; ask for the more compact ubjson.
	req.Header.Set("Accept", "application/ubjson")
	// Close the connection after each poll. The device serves connections one at
	// a time (max_tasks=1), so a kept-alive poll connection would pin its listen
	// task across polls and starve concurrent /run and /ping requests.
	req.Close = true

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got non-OK from device: %s", res.Status)
	}

	var wire logPollWire
	if err := ubjson.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	parsed := &LogPollResponse{Next: wire.Next, Head: wire.Head, Oldest: wire.Oldest}
	for _, raw := range wire.Entries {
		entry, err := logEntryFromArray(raw)
		if err != nil {
			return nil, err
		}
		parsed.Entries = append(parsed.Entries, entry)
	}
	return parsed, nil
}

// ConfigureLog applies a log-buffer configuration via PUT /log/configure. The
// config map mirrors the /log/configure body (e.g. {"enabled": true,
// "buffer_size": 4096, "min_level": "INFO"}); missing fields leave the matching
// device setting unchanged.
func (d DeviceNetwork) ConfigureLog(ctx context.Context, config map[string]interface{}) error {
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	req, err := d.newRequest(ctx, "PUT", "/log/configure", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set(JaguarDeviceIDHeader, d.ID())
	// The device serves one connection at a time; don't pin it across the call.
	req.Close = true
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	io.ReadAll(res.Body) // Avoid closing connection prematurely.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("got non-OK from device: %s", res.Status)
	}
	return nil
}
