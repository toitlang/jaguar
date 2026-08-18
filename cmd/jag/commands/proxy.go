// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"time"

	"go.bug.st/serial"
)

// A small HTTP server that can be used to communicate with the device through
// the UART.

// A sequence of random numbers that is used as synchronization token.
var syncMagic = []byte{27, 121, 55, 49, 253, 65, 123, 243}

const defaultProxyBaudRate = 921600

func uartName(name string) string {
	return name + "-uart"
}

func runUartProxy(dev *serialPort, reader HasDataReader, setDefaultBaudRate bool) error {
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

	if setDefaultBaudRate && identity.CanChangeBaudRate {
		changed, err := ud.SetBaudRate(defaultProxyBaudRate, func(baudRate int) error {
			return dev.SetMode(&serial.Mode{BaudRate: baudRate})
		})
		if err != nil {
			return err
		}
		if changed {
			// The device changes its baud rate shortly after acknowledging the request.
			time.Sleep(200 * time.Millisecond)
			if err := ud.Ping(); err != nil {
				return err
			}
			fmt.Printf("[jaguar.uart] INFO: switched UART proxy to %d baud.\n", defaultProxyBaudRate)
		}
	}

	return runProxyServer(ud, identity)
}
