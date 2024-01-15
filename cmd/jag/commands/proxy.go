// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"io"
)

// A small HTTP server that can be used to communicate with the device through
// the UART.

// A sequence of random numbers that is used as synchronization token.
var syncMagic = []byte{27, 121, 55, 49, 253, 65, 123, 243}

func uartName(name string) string {
	return name + "-uart"
}

func runUartProxy(dev *serialPort, reader io.Reader) error {
	ud := newUartDevice(dev, reader)

	err := ud.Sync()
	if err != nil {
		return err
	}

	identity, err := ud.Identify()
	if err != nil {
		// TODO(florian): this print should be a log.
		fmt.Println("Identify error")
		return err
	}

	return runProxyServer(ud, identity)
}
