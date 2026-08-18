// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"bytes"
	"testing"
)

type testHasDataReader struct {
	*bytes.Reader
}

func (r *testHasDataReader) HasData() bool {
	return false
}

func newTestUartDevice(writer *bytes.Buffer, reader HasDataReader) *uartDevice {
	return &uartDevice{
		writer:           writer,
		underlyingReader: reader,
		bufferedReader:   bufio.NewReader(reader),
	}
}

func TestSetBaudRate(t *testing.T) {
	response := buildRequest(commandSetBaudRate, []byte{1})
	reader := &testHasDataReader{bytes.NewReader(response)}
	writer := &bytes.Buffer{}
	device := newTestUartDevice(writer, reader)

	setBaudRate := 0
	changed, err := device.SetBaudRate(defaultProxyBaudRate, func(baudRate int) error {
		setBaudRate = baudRate
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("baud rate was not changed")
	}
	if setBaudRate != defaultProxyBaudRate {
		t.Fatalf("set baud rate to %d, want %d", setBaudRate, defaultProxyBaudRate)
	}

	want := buildRequest(commandSetBaudRate, []byte{0x00, 0x10, 0x0e, 0x00})
	if !bytes.Equal(writer.Bytes(), want) {
		t.Fatalf("request %v, want %v", writer.Bytes(), want)
	}
}

func TestSetBaudRateUnsupported(t *testing.T) {
	response := buildRequest(commandSetBaudRate, []byte{0})
	reader := &testHasDataReader{bytes.NewReader(response)}
	device := newTestUartDevice(&bytes.Buffer{}, reader)

	changed, err := device.SetBaudRate(defaultProxyBaudRate, func(int) error {
		t.Fatal("host baud rate changed for an unsupported endpoint")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("baud rate reported as changed")
	}
}
