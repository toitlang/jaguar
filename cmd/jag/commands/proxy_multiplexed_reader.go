// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"encoding/base64"
	"io"
)

const (
	MagicToken = "Jag15261520"
)

// multiplexReader takes an io.Reader and returns two readers.
// The first reader contains usual output and also gets any error.
// The second reader contains the out-of-band messages, marked with the MagicToken.
func multiplexReader(input io.Reader) (reader1, reader2 *chanReader) {
	// Create channels for communicating data to readers.
	ch1 := make(chan []byte, 100)
	ch2 := make(chan []byte, 100)
	errc := make(chan error, 1)

	go func() {
		defer close(ch1)
		defer close(ch2)

		acc := []byte{}
		chunk := make([]byte, 1024)
	ReadLoop:
		for {
			count, err := input.Read(chunk)
			if err != nil {
				errc <- err
				ch1 <- nil
				ch2 <- nil
				return
			}
			acc = append(acc, chunk[:count]...)
		SearchMagic:
			for len(acc) > 0 {
				// Find the magic token in the accumulated data.
				// Start by looking for the first byte of the token.
				idx := bytes.IndexByte(acc, MagicToken[0])
				if idx == -1 {
					// No magic token found, send all accumulated data to reader2.
					ch1 <- acc
					acc = []byte{}
					break
				}
				// Send all data up to the first byte of the token to reader2.
				if idx > 0 {
					ch1 <- acc[:idx]
					acc = acc[idx:]
				}
				// Check if the rest of the token is in the accumulated data.
				i := 0
				for ; i < len(MagicToken)-1; i++ {
					if i >= len(acc) {
						break
					}
					if acc[i] != MagicToken[i] {
						// Doesn't match.
						// Try again from the next byte.
						ch1 <- acc[:i+1]
						acc = acc[i+1:]
						continue SearchMagic
					}
				}
				// We have a magic token, or at least the beginning of it.
				if len(acc) < len(MagicToken) {
					// Not enough data to check the rest of the token.
					// Read more.
					continue ReadLoop
				}
				afterMagic := acc[len(MagicToken):]
				// Find the end of the packet.
				newLineIndex := bytes.IndexByte(afterMagic, '\n')
				if newLineIndex == -1 {
					// Read more.
					continue ReadLoop
				}
				// Send the data to reader1.
				encodedData := afterMagic[:newLineIndex]
				data, err := base64.StdEncoding.DecodeString(string(encodedData))
				if err != nil {
					continue
				}
				ch2 <- data
				acc = afterMagic[newLineIndex+1:]
				continue SearchMagic
			}
		}
	}()

	reader1 = &chanReader{ch: ch1, errc: errc}
	reader2 = &chanReader{ch: ch2}

	return reader1, reader2
}

// chanReader implements io.Reader by reading from a channel
type chanReader struct {
	ch   <-chan []byte
	errc <-chan error
}

func (cr *chanReader) Read(p []byte) (n int, err error) {
	data, ok := <-cr.ch
	if !ok {
		return 0, io.EOF
	}

	if data == nil {
		if cr.errc == nil {
			return 0, io.EOF
		}
		println("Error")
		return 0, <-cr.errc
	}

	if p == nil {
		return len(data), nil
	}
	copy(p, data)
	return len(data), nil
}

func (cr *chanReader) HasData() bool {
	return len(cr.ch) > 0
}
