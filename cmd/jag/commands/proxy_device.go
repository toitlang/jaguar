// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
	"time"

	"github.com/toitware/ubjson"
)

const (
	// Commands.
	commandSync           = 0
	commandPing           = 1
	commandIdentify       = 2
	commandListContainers = 3
	commandUninstall      = 4
	commandFirmware       = 5
	commandInstall        = 6
	commandRun            = 7

	responseAck = 255
)

type HasDataReader interface {
	io.Reader
	HasData() bool
}

type uartDevice struct {
	lock             sync.Mutex
	writer           io.Writer
	underlyingReader HasDataReader
	bufferedReader   *bufio.Reader
	syncId           int
}

func newUartDevice(writer io.Writer, reader HasDataReader) *uartDevice {
	result := &uartDevice{
		writer:           writer,
		underlyingReader: reader,
		bufferedReader:   bufio.NewReader(reader),
	}
	go func() {
		for {
			// Synchronize every 5 seconds.
			time.Sleep(5 * time.Second)
			result.Sync()
		}
	}()
	return result
}

// Sync synchronizes the device with the server.
// The server repeatedly sends a sync request to the device, and the device
// responds with a sync response.
func (d *uartDevice) Sync() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.sync()
}

func (d *uartDevice) hasIncomingData() bool {
	return d.underlyingReader.HasData() || d.bufferedReader.Buffered() > 0
}

func (d *uartDevice) sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Drain any data that is buffered so far.
	for d.underlyingReader.HasData() {
		_, err := d.underlyingReader.Read(nil)
		if err != nil {
			return err
		}
	}
	if d.bufferedReader.Buffered() > 0 {
		d.bufferedReader.Reset(d.underlyingReader)
	}

	errc := make(chan error)
	successc := make(chan struct{})
	stopc := make(chan struct{})
	done := false

	go func() {
		for !done {
			// Use the '\n' of messages to align the reader.
			data, err := d.bufferedReader.ReadBytes('\n')
			if err != nil {
				errc <- err
				return
			}
			if len(data) == 0 {
				continue
			}
			expectedSyncPacket := buildExpectedSyncResponse(d.syncId)
			if !bytes.Equal(data, expectedSyncPacket) {
				// Discard the data.
				continue
			}
			successc <- struct{}{}
			return
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-stopc:
				return
			default:
				// Send a sync request.
				// Increment before building, so that the read function can use the id without adjustments.
				d.syncId++
				syncRequest := buildSyncRequest(d.syncId)
				err := d.writeAll(syncRequest)
				if err != nil {
					errc <- err
					return
				}
				time.Sleep(2 * time.Second)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			done = true
			stopc <- struct{}{}
			return ctx.Err()
		case err := <-errc:
			done = true
			stopc <- struct{}{}
			return err
		case <-successc:
			done = true
			stopc <- struct{}{}
			return nil
		}
	}
}

func (d *uartDevice) Ping() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	response, err := d.sendRequest(commandPing, []byte{})
	if err != nil {
		return err
	}
	if len(response) != 0 {
		return fmt.Errorf("invalid ping response")
	}
	return nil
}

type uartIdentity struct {
	Name       string `json:"name"`
	Id         string `json:"id"`
	Chip       string `json:"chip"`
	SdkVersion string `json:"sdkVersion"`
}

func (d *uartDevice) Identify() (*uartIdentity, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	response, err := d.sendRequest(commandIdentify, []byte{})
	if err != nil {
		return nil, err
	}
	var identity uartIdentity
	err = ubjson.Unmarshal(response, &identity)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (d *uartDevice) ListContainers() ([]byte, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.sendRequest(commandListContainers, []byte{})
}

func (d *uartDevice) Uninstall(containerName string) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	_, err := d.sendRequest(commandUninstall, []byte(containerName))
	return err
}

func (d *uartDevice) Firmware(newFirmware []byte) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(len(newFirmware)))
	_, err := d.sendRequest(commandFirmware, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(newFirmware)
}

func (d *uartDevice) Install(containerName string, defines map[string]interface{}, containerImage []byte) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	encodedDefines, err := ubjson.Marshal(defines)
	if err != nil {
		return err
	}
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(len(containerImage)))
	payload = appendUint32Le(payload, crc32.ChecksumIEEE(containerImage))
	payload = appendUint16Le(payload, uint16(len(containerName)))
	payload = append(payload, containerName...)
	payload = append(payload, encodedDefines...)

	_, err = d.sendRequest(commandInstall, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(containerImage)
}

func (d *uartDevice) Run(defines map[string]interface{}, image []byte) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	encodedDefines, err := ubjson.Marshal(defines)
	if err != nil {
		return err
	}
	payload := []byte{}
	payload = appendUint32Le(payload, uint32(len(image)))
	payload = appendUint32Le(payload, crc32.ChecksumIEEE(image))
	payload = append(payload, encodedDefines...)

	_, err = d.sendRequest(commandRun, payload)
	if err != nil {
		return err
	}
	return d.streamChunked(image)
}

func appendUint16Le(data []byte, value uint16) []byte {
	data = append(data, byte(value&0xff))
	data = append(data, byte(value>>8))
	return data
}

func appendUint32Le(data []byte, value uint32) []byte {
	data = append(data, byte(value&0xff))
	data = append(data, byte(value>>8))
	data = append(data, byte(value>>16))
	data = append(data, byte(value>>24))
	return data
}

func (d *uartDevice) streamChunked(data []byte) error {
	length := len(data)
	// Start sending the image in chunks of 512 bytes.
	// Expect a response for each chunk.
	written := 0
	for written < int(length) {
		chunkSize := 512
		if written+chunkSize > int(length) {
			chunkSize = int(length) - written
		}
		chunk := data[written : written+chunkSize]
		d.writeAll(chunk)
		written += chunkSize
		consumed := 0
		for consumed < chunkSize {
			// Read the Ack. 3 bytes.
			buffer := make([]byte, 3)
			_, err := io.ReadFull(d.bufferedReader, buffer)
			if err != nil {
				return err
			}
			if buffer[0] != responseAck {
				return fmt.Errorf("invalid ack")
			}
			consumed += int(buffer[1]) | (int(buffer[2]) << 8)
			if consumed > chunkSize {
				return fmt.Errorf("invalid ack")
			}
		}
	}
	return nil
}

func (d *uartDevice) sendRequest(command byte, payload []byte) ([]byte, error) {
	// There should be no data in the reader. If there is, we sync first.
	if d.hasIncomingData() {
		err := d.sync()
		if err != nil {
			return nil, err
		}
	}
	err := d.WritePacket(append([]byte{command}, payload...))
	if err != nil {
		return nil, err
	}
	return d.ReceiveResponse(command)
}

// WritePacket writes the given data as a packet.
// Packets start with their payload-length, followed by the payload, and end
// with a zero byte.
func (d *uartDevice) WritePacket(data []byte) error {
	if len(data) > 65535 {
		return io.ErrShortBuffer
	}
	length := len(data)
	err := d.writeAll([]byte{byte(length & 0xff), byte(length >> 8)})
	if err != nil {
		return err
	}
	err = d.writeAll(data)
	if err != nil {
		return err
	}
	return d.writeAll([]byte{'\n'})
}

func (d *uartDevice) writeAll(data []byte) error {
	for len(data) > 0 {
		count, err := d.writer.Write(data)
		if err != nil {
			return err
		}
		data = data[count:]
	}
	return nil
}

// ReceiveResponse receives a response to the given command.
// The returned data is the payload of the response, without the command byte.
func (d *uartDevice) ReceiveResponse(command byte) ([]byte, error) {
	lengthLsb, err := d.bufferedReader.ReadByte()
	if err != nil {
		return nil, err
	}
	lengthMsb, err := d.bufferedReader.ReadByte()
	if err != nil {
		return nil, err
	}
	payloadLength := int(lengthLsb) | (int(lengthMsb) << 8)
	payload := make([]byte, payloadLength)
	_, err = io.ReadFull(d.bufferedReader, payload)
	if err != nil {
		return nil, err
	}
	// Make sure the packet ends with a zero byte.
	b, err := d.bufferedReader.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != '\n' {
		return nil, fmt.Errorf("invalid packet terminator")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	if payload[0] != command {
		return nil, fmt.Errorf("unmatched response")
	}
	return payload[1:], nil
}

func buildRequest(command byte, data []byte) []byte {
	payload := []byte{}
	payload = append(payload, command)
	payload = append(payload, data...)
	payloadSize := len(payload)
	if payloadSize > 65535 {
		panic("payload too large")
	}
	request := []byte{}
	request = appendUint16Le(request, uint16(payloadSize))
	request = append(request, payload...)
	request = append(request, '\n')
	return request
}

func buildSyncRequest(syncId int) []byte {
	data := []byte{}
	data = appendUint16Le(data, uint16(syncId))
	for i := 0; i < len(syncMagic); i++ {
		// The magic number is sent with each byte decremented by one to avoid
		// accidental detections.
		data = append(data, syncMagic[i]-1)
	}
	return buildRequest(commandSync, data)
}

func buildExpectedSyncResponse(syncId int) []byte {
	response := []byte{}
	payload := []byte{}
	payload = append(payload, commandSync)
	payload = appendUint16Le(payload, uint16(syncId))

	response = appendUint16Le(response, uint16(len(payload)))
	response = append(response, payload...)
	response = append(response, '\n')
	return response
}
