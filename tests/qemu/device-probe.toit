// Copyright (C) 2026 Toit contributors.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import net.wifi

main:
  network := wifi.open --ssid="Open Wifi" --password=""
  try:
    identity/string? := null
    attempts := 0
    while not identity and attempts++ < 100:
      catch:
        socket := network.tcp-connect network.address.stringify 9000
        try:
          socket.out.write """
              GET /identify HTTP/1.0\r
              Host: jaguar\r
              \r
              """ --flush
          response := socket.in.read-all.to-string
          marker := "\"id\": \""
          start := response.index-of marker
          if start >= 0:
            start += marker.size
            tail := response[start..]
            end := tail.index-of "\""
            if end > 0: identity = tail[..end]
        finally:
          socket.close
      sleep --ms=100
    if not identity: throw "Jaguar identify endpoint did not become ready"

    socket := network.tcp-connect network.address.stringify 9000
    try:
      socket.out.write """
          GET /ping HTTP/1.0\r
          Host: jaguar\r
          X-Jaguar-Device-ID: $identity\r
          \r
          """ --flush
      response := socket.in.read-all.to-string
      if not response.starts-with "HTTP/1.1 200":
        throw "Jaguar ping did not return HTTP 200"
      print "JAGUAR-QEMU-DEVICE: PASS"
    finally:
      socket.close
  finally:
    network.close
