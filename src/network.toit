// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.ubjson
import http
import log
import monitor
import net
import net.udp
import net.tcp
import system
import system.firmware
import uuid show Uuid

import .jaguar

HTTP-PORT        ::= 9000
IDENTIFY-PORT    ::= 1990
IDENTIFY-ADDRESS ::= net.IpAddress.parse "255.255.255.255"
STATUS-OK-JSON   ::= """{ "status": "OK" }"""

HEADER-DEVICE-ID         ::= "X-Jaguar-Device-ID"
HEADER-SDK-VERSION       ::= "X-Jaguar-SDK-Version"
HEADER-WIFI-DISABLED     ::= "X-Jaguar-Wifi-Disabled"
HEADER-CONTAINER-NAME    ::= "X-Jaguar-Container-Name"
HEADER-CONTAINER-TIMEOUT ::= "X-Jaguar-Container-Timeout"
HEADER-CRC32             ::= "X-Jaguar-CRC32"

// Assets for the mini-webpage that the device serves up on $HTTP_PORT.
CHIP-IMAGE ::= "https://toitlang.github.io/jaguar/device-files/chip.svg"
STYLE-CSS ::= "https://toitlang.github.io/jaguar/device-files/style.css"

class EndpointHttp implements Endpoint:
  logger/log.Logger

  constructor logger/log.Logger:
    this.logger = logger.with-name "http"

  uses-network -> bool:
    return true

  run device/Device:
    logger.debug "starting endpoint"
    network ::= net.open
    socket/tcp.ServerSocket? := null
    try:
      socket = network.tcp-listen device.port
      address := "http://$network.address:$socket.local-address.port"
      logger.info "running Jaguar device '$device.name' (id: '$device.id') on '$address'"

      // We've successfully connected to the network, so we consider
      // the current firmware functional. Go ahead and validate the
      // firmware if requested to do so.
      validate-firmware --reason="connected to network"

      request-mutex := monitor.Mutex

      // We run two tasks concurrently: One broadcasts the device identity
      // via UDP and one serves incoming HTTP requests. We run the tasks
      // in a group so if one of them terminates, we take the other one down
      // and clean up nicely.
      Task.group --required=1 [
        :: broadcast-identity network device address,
        :: serve-incoming-requests socket device address request-mutex,
        // If the call to the network-manager monitor returns, it will terminate the
        // task and thus shut down the whole group.
        ::
          network-manager.wait-for-request-to-disable-network
          request-mutex.do:
            // Get the lock so that we know that the last request has been handled.
            socket.close
            socket = null
      ]
    finally:
      if socket: socket.close
      network.close

  identity-payload device/Device address/string -> ByteArray:
    identity := """
      { "method": "jaguar.identify",
        "payload": {
          "name": "$device.name",
          "id": "$device.id",
          "chip": "$device.chip",
          "sdkVersion": "$system.vm-sdk-version",
          "address": "$address",
          "wordSize": $system.BYTES-PER-WORD
        }
      }
    """
    return identity.to-byte-array

  broadcast-identity network/net.Interface device/Device address/string -> none:
    payload ::= identity-payload device address
    datagram ::= udp.Datagram
        payload
        net.SocketAddress IDENTIFY-ADDRESS IDENTIFY-PORT
    socket := network.udp-open
    try:
      socket.broadcast = true
      while not network.is-closed:
        socket.send datagram
        sleep --ms=200
    finally:
      socket.close

  handle-browser-request name/string request/http.Request writer/http.ResponseWriter -> none:
    path := request.path
    if path == "/": path = "index.html"
    if path.starts-with "/": path = path[1..]

    if path == "index.html":
      uptime ::= Duration --s=(Time.monotonic-us --since-wakeup) / Duration.MICROSECONDS-PER-SECOND

      writer.headers.set "Content-Type" "text/html"
      writer.out.write """
          <html>
            <head>
              <link rel="stylesheet" href="$STYLE-CSS">
              <title>$name (Jaguar device)</title>
            </head>
            <body>
              <div class="box">
                <section class="text-center">
                  <img src="$CHIP-IMAGE" alt="Picture of an embedded device" width=200>
                </section>
                <h1 class="mt-40">$name</h1>
                <p class="text-center">Jaguar device</p>
                <p class="hr mt-40"></p>
                <section class="grid grid-cols-2 mt-20">
                  <p>Uptime</p>
                  <p><b class="text-black">$uptime</b></p>
                  <p>SDK</p>
                  <p><b class="text-black">$system.vm-sdk-version</b></p>
                </section>
                <p class="hr mt-20"></p>
                <p class="mt-40">Run code on this device using</p>
                <b><a href="https://github.com/toitlang/jaguar">&gt; jag run -d $name hello.toit</a></b>
                <p class="mt-20">Monitor the serial port console using</p>
                <p class="mb-20"><b><a href="https://github.com/toitlang/jaguar">&gt; jag monitor</a></b></p>
              </div>
            </body>
          </html>
          """
    else if path == "favicon.ico":
      writer.redirect http.STATUS-FOUND CHIP-IMAGE
    else:
      writer.headers.set "Content-Type" "text/plain"
      writer.write-headers http.STATUS-NOT-FOUND
      writer.out.write "Not found: $path"

  serve-incoming-requests socket/tcp.ServerSocket device/Device address/string request-mutex/monitor.Mutex -> none:
    self := Task.current

    server := http.Server --logger=logger --read-timeout=(Duration --s=3)

    server.listen socket:: | request/http.Request writer/http.ResponseWriter |
      headers ::= request.headers
      device-id := "$device.id"
      device-id-header := headers.single HEADER-DEVICE-ID
      sdk-version-header := headers.single HEADER-SDK-VERSION
      path := request.path

      // Handle identification requests before validation, as the caller doesn't know that information yet.
      if path == "/identify" and request.method == http.GET:
        writer.headers.set "Content-Type" "application/json"
        result := identity-payload device address
        writer.headers.set "Content-Length" result.size.stringify
        writer.out.write result

      else if path == "/" or path.ends-with ".html" or path.ends-with ".css" or path.ends-with ".ico":
        handle-browser-request device.name request writer

      // Validate device ID.
      else if device-id-header != device-id:
        logger.info "denied request, header: '$HEADER-DEVICE-ID' was '$device-id-header' not '$device-id'"
        writer.write-headers http.STATUS-FORBIDDEN --message="Device has id '$device-id', jag is trying to talk to '$device-id-header'"

      // Handle pings.
      else if path == "/ping" and request.method == http.GET:
        respond-ok writer

      // Handle listing containers.
      else if path == "/list" and request.method == http.GET:
        result := ubjson.encode registry_.entries
        writer.headers.set "Content-Type" "application/ubjson"
        writer.headers.set "Content-Length" result.size.stringify
        writer.out.write result

      // Handle uninstalling containers.
      else if path == "/uninstall" and request.method == http.PUT:
        request-mutex.do:
          container-name ::= headers.single HEADER-CONTAINER-NAME
          uninstall-image container-name
          respond-ok writer

      // Handle firmware updates.
      else if path == "/firmware" and request.method == http.PUT:
        request-mutex.do:
          install-firmware request.content-length request.body
          respond-ok writer
          // Mark the firmware as having a pending upgrade and close
          // the server socket to force the HTTP server loop to stop.
          firmware-is-upgrade-pending = true
          socket.close

      // Validate SDK version before attempting to install containers or run code.
      else if sdk-version-header != system.vm-sdk-version:
        logger.info "denied request, header: '$HEADER-SDK-VERSION' was '$sdk-version-header' not '$system.vm-sdk-version'"
        writer.write-headers http.STATUS-NOT-ACCEPTABLE --message="Device has $system.vm-sdk-version, jag has $sdk-version-header"

      // Handle installing containers and code running.
      else if (path == "/install" or path == "/run") and request.method == "PUT":
        image/Uuid? := null
        defines/Map? := null
        container-name/string? := null
        request-mutex.do:
          container-name = (path == "/install")
              ? headers.single HEADER-CONTAINER-NAME
              : null
          crc32 := int.parse (headers.single HEADER-CRC32)
          defines = extract-defines headers
          image = flash-image request.content-length request.body container-name defines --crc32=crc32
          respond-ok writer
        run-message := path == "/install" ? "installed and started" : "started"
        run-image image run-message container-name defines

  extract-defines headers/http.Headers -> Map:
    defines := {:}
    if headers.single HEADER-WIFI-DISABLED:
      defines[JAG-WIFI] = false
    if header := headers.single HEADER-CONTAINER-TIMEOUT:
      timeout := int.parse header --on-error=: null
      if timeout: defines[JAG-TIMEOUT] = timeout
    return defines

  respond-ok writer/http.ResponseWriter -> none:
    writer.headers.set "Content-Type" "application/json"
    writer.headers.set "Content-Length" STATUS-OK-JSON.size.stringify
    writer.out.write STATUS-OK-JSON

  name -> string:
    return "HTTP"
