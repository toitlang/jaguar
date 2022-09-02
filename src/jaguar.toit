// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json
import device
import http
import log
import net
import net.udp
import net.tcp
import reader
import esp32
import uuid
import monitor

import system.containers
import system.firmware

import .container_registry

HTTP_PORT ::= 9000
IDENTIFY_PORT ::= 1990
IDENTIFY_ADDRESS ::= net.IpAddress.parse "255.255.255.255"

DEVICE_ID_HEADER ::= "X-Jaguar-Device-ID"
SDK_VERSION_HEADER ::= "X-Jaguar-SDK-Version"
RUN_DEFINES_HEADER ::= "X-Jaguar-Run-Defines"

logger ::= log.Logger log.INFO_LEVEL log.DefaultTarget --name="jaguar"
validate_firmware / bool := firmware.is_validation_pending

/**
Jaguar can run containers while Jaguar itself is disabled. You can
  enable this behavior by using `jag run -D jag.disabled ...` when
  starting the container. Use this mode to test how your apps behave
  when they run with no pre-established network.

We keep track of the state through the global $disabled variable and
  we set this to true when starting a container that needs to run with
  Jaguar disabled. In return, this makes the outer $serve loop wait
  for the container to be done, before it re-establishes the network
  connection and restarts the HTTP server.
*/
disabled / bool := false
network_free   / monitor.Semaphore ::= monitor.Semaphore
container_done / monitor.Semaphore ::= monitor.Semaphore

// The installed and named containers are kept in a registry backed
// by the flash (on the device).
registry_ / ContainerRegistry ::= ContainerRegistry

main arguments:
  try:
    exception := catch --trace: serve arguments
    logger.error "rebooting due to $(exception)"
  finally:
    esp32.deep_sleep (Duration --s=1)

serve arguments:
  port := HTTP_PORT
  if arguments.size >= 1:
    port = int.parse arguments[0]

  image_config := {:}
  if platform == PLATFORM_FREERTOS:
    image_config = esp32.image_config or {:}

  id/uuid.Uuid := uuid.NIL
  if arguments.size >= 2:
    id = uuid.parse arguments[1]
  else:
    id = image_config.get "id"
      --if_absent=: id
      --if_present=: uuid.parse it

  name/string := "unknown"
  if arguments.size >= 3:
    name = arguments[2]
  else:
    name = image_config.get "name" --if_absent=: name

  while true:
    attempts ::= 3
    failures := 0
    while failures < attempts:
      exception := catch: run id name port
      if disabled:
        network_free.up      // Signal to start running the container.
        container_done.down  // Wait until done running the container.
        disabled = false
      if not exception: continue
      failures++
      logger.warn "running Jaguar failed due to '$exception' ($failures/$attempts)"
    // If we need to validate the firmware and we've failed to do so
    // in the first round of attempts, we roll back to the previous
    // firmware right away.
    if validate_firmware:
      logger.error "firmware update was rejected after failing to connect or validate"
      firmware.rollback
    backoff := Duration --s=5
    logger.info "backing off for $backoff"
    sleep backoff

run id/uuid.Uuid name/string port/int:
  broadcast_task := null
  server_task := null
  network/net.Interface? := null
  error := null

  socket/tcp.ServerSocket? := null
  try:
    network = net.open
    socket = network.tcp_listen port
    address := "http://$network.address:$socket.local_address.port"
    logger.info "running Jaguar device '$name' (id: '$id') on '$address'"

    // We've successfully connected to the network, so we consider
    // the current firmware functional. Go ahead and validate the
    // firmware if requested to do so.
    if validate_firmware:
      if firmware.validate:
        logger.info "firmware update validated after connecting to network"
        validate_firmware = false
      else:
        logger.error "firmware update failed to validate"

    // We run two tasks concurrently: One broadcasts the device identity
    // via UDP and one serves incoming HTTP requests. If one of the tasks
    // fail, we take the other one down to clean up nicely.
    done := monitor.Semaphore
    server_task = task::
      try:
        error = catch: serve_incoming_requests socket id name address
      finally:
        server_task = null
        if broadcast_task: broadcast_task.cancel
        critical_do: done.up

    broadcast_task = task::
      try:
        error = catch: broadcast_identity network id name address
      finally:
        broadcast_task = null
        if server_task: server_task.cancel
        critical_do: done.up

    // Wait for both tasks to finish.
    2.repeat: done.down

  finally:
    if socket: socket.close
    if network: network.close
    if error: throw error

install_mutex ::= monitor.Mutex

install_image image_size/int reader/reader.Reader defines/Map -> none:
  timeout/Duration? := null
  jag_timeout := defines.get "jag.timeout"
  if jag_timeout is string:
    value := int.parse jag_timeout[0..jag_timeout.size - 1] --on_error=(: 0)
    if value > 0 and jag_timeout.ends_with "s":
      timeout = Duration --s=value
    else if value > 0 and jag_timeout.ends_with "m":
      timeout = Duration --m=value
    else if value > 0 and jag_timeout.ends_with "h":
      timeout = Duration --h=value
    else:
      logger.error "invalid jag.timeout setting (\"$jag_timeout\")"
  else if jag_timeout is int and jag_timeout > 0:
    timeout = Duration --s=jag_timeout
  else if jag_timeout:
    logger.error "invalid jag.timeout setting ($jag_timeout)"

  jag_disabled := defines.get "jag.disabled"
  if jag_disabled:
    if not timeout: timeout = Duration --s=10
    disabled = true

  name/string? := defines.get "container.name" --if_absent=: null

  with_timeout --ms=60_000: install_mutex.do:
    image := registry_.install name:
      logger.debug "installing container image with $image_size bytes"
      written_size := 0
      writer := containers.ContainerImageWriter image_size
      while data := reader.read:
        written_size += data.size
        writer.write data
      logger.debug "installing container image with $image_size bytes -> wrote $written_size bytes"
      writer.commit --run_boot=(name != null)

    // We start the container from a separate task to allow the HTTP server
    // to continue operating. This also means that the container running
    // isn't covered by the installation mutex or associated timeout.
    task::
      // First, we wait until we're ready to run the container. Usually,
      // we are ready right away, but if we've been asked to disable
      // Jaguar while running the container, we wait until the HTTP server
      // has been shut down and the network to be free.
      if disabled: network_free.down

      suffix := defines.is_empty ? "" : " with $defines"
      logger.info "container $image started$suffix"
      start ::= Time.monotonic_us
      container ::= containers.start image

      // We're only interested in handling the timeout errors, so we
      // unwind and produce a stack trace in all other cases.
      filter ::= : it != DEADLINE_EXCEEDED_ERROR

      // Wait until the container is done or until we time out.
      code/int? := null
      catch --unwind=filter --trace=filter:
        with_timeout timeout: code = container.wait
      if not code:
        elapsed ::= Duration --us=Time.monotonic_us - start
        code = container.stop
        logger.info "container $image timed out after $elapsed"

      if code == 0:
        logger.info "container $image stopped"
      else:
        logger.error "container $image stopped - exit code $code"

      // If Jaguar was disabled while running the container, now is the
      // time to restart the HTTP server.
      if disabled: container_done.up

uninstall_image defines/Map -> none:
  name/string := defines["container.name"]
  with_timeout --ms=60_000: install_mutex.do:
    if id := registry_.uninstall name:
      logger.info "container $id uninstalled ('$name')"
    else:
      logger.error "container '$name' not found"

install_firmware firmware_size/int reader/reader.Reader -> none:
  with_timeout --ms=120_000: install_mutex.do:
    logger.info "installing firmware with $firmware_size bytes"
    written_size := 0
    writer := firmware.FirmwareWriter 0 firmware_size
    try:
      last := null
      while data := reader.read:
        written_size += data.size
        writer.write data
        percent := (written_size * 100) / firmware_size
        if percent != last:
          logger.info "installing firmware with $firmware_size bytes ($percent%)"
          last = percent
      writer.commit
      logger.info "installed firmware; rebooting"
    finally:
      writer.close

identity_payload id/uuid.Uuid name/string address/string -> ByteArray:
  return json.encode {
    "method": "jaguar.identify",
    "payload": {
      "name": name,
      "id": id.stringify,
      "sdkVersion": vm_sdk_version,
      "address": address,
      "wordSize": BYTES_PER_WORD,
    }
  }

broadcast_identity network/net.Interface id/uuid.Uuid name/string address/string -> none:
  payload ::= identity_payload id name address
  datagram ::= udp.Datagram
      payload
      net.SocketAddress IDENTIFY_ADDRESS IDENTIFY_PORT
  socket := network.udp_open
  try:
    socket.broadcast = true
    while not network.is_closed:
      socket.send datagram
      sleep --ms=200
  finally:
    socket.close

handle_browser_request request/http.Request writer/http.ResponseWriter -> none:
  path := request.path
  if path == "/": path = "index.html"
  if path.starts_with "/": path = path[1..]
  CHIP_IMAGE ::= "https://toit.io/static/chip-e4ce030bdea3996fa7ad44ff63d88e52.svg"

  if path == "index.html":
    uptime ::= Duration --s=Time.monotonic_us / Duration.MICROSECONDS_PER_SECOND

    writer.headers.set "Content-Type" "text/html"
    writer.write """
        <html>
          <head>
            <link rel="stylesheet" href="style.css">
            <title>$device.name (Jaguar device)</title>
          </head>
          <body>
            <div class="box">
              <section class="text-center">
                <img src="$CHIP_IMAGE" alt="Picture of an embedded device" width=200 />
              </section>
              <h1 class="mt-40">$device.name</h1>
              <p class="text-center">Jaguar device<p>
              <p class="hr mt-40"></p>
              <section class="grid grid-cols-2 mt-20">
                <p>Uptime</p>
                <p><b class="text-black">$uptime</b></p>
                <p>SDK</p>
                <p><b class="text-black">$vm_sdk_version</b></p>
              </section>
              <p class="hr mt-20"></p>
              <p class="mt-40">Run code on this device using</p>
              <b><a href="https://github.com/toitlang/jaguar">&gt; jag run</a></b>
              <p class="mt-20">Monitor the serial port console using</p>
              <p class="mb-20"><b><a href="https://github.com/toitlang/jaguar">&gt; jag monitor</a></b></p>
            </div>
          </body>
        </html>
        """
  else if path == "style.css":
    writer.headers.set "Content-Type" "text/css"
    writer.write """
        body {
          background-color: #F8FAFC;
          color: #444;
        }
        h1 {
          font-family: -apple-system, "Helvetica Neue", Arial;
          text-align: center;
          font-size: 40px;
          margin-top: 0;
          margin-bottom: 15px;
          color: #444;
        }
        p {
          margin: 0;
        }
        .box {
          position: relative;
          border: none;
          background: #fff;
          border-radius: 16px;
          box-shadow: #FFF 0 0 0 0 inset, #00000019 0 0 0 1px inset,
          #0000 0 0 0 0, #0000 0 0 0 0, #E2E8F0 0 20px 25px -5px, #E2E8F0 0 8px 10px -6px;
          box-sizing: border-box;
          display: block;
          line-height: 24px;
          padding: 12px;
          width: 360px;
          margin: auto;
          margin-top: 60px;
          padding-left: 20px;
        }
        .icon {
          padding-top: 20px;
          color: #55A398;
          position: relative;
          width: 140px;
        }
        p, div {
          -webkit-font-smoothing: antialiased;
          font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
          font-size: 14px;
          color: #64748B;
          margin: 0;
        }
        .text-center {
          text-align: center;
        }
        .hr {
          -webkit-font-smoothing: antialiased;
          background-image: linear-gradient(to right, #E2E8F000, #E2E8F0, #E3E8F000);
          height: 1px;
          width: 100%;
        }
        a {
          color: #55A398;
        }
        a:link {
          text-decoration: none;
          color: #55A398;
        }
        a:hover {
          text-decoration: underline;
        }
        .text-black {
          color: #000;
        }
        .mt-40 {
          margin-top: 40px;
        }
        .mt-20 {
          margin-top: 20px;
        }
        .mb-20 {
          margin-bottom: 20px;
        }
        .grid {
          display: grid;
        }
        .grid-cols-2	 {
          grid-template-columns: 1fr 3fr;
        }
        """
  else if path == "favicon.ico":
    writer.headers.set "Location" CHIP_IMAGE
    writer.write_headers 302
  else:
    writer.headers.set "Content-Type" "text/plain"
    writer.write_headers 404
    writer.write "Not found: $path"

serve_incoming_requests socket/tcp.ServerSocket id/uuid.Uuid name/string address/string -> none:
  self := Task.current

  server := http.Server --logger=logger
  server.listen socket:: | request/http.Request writer/http.ResponseWriter |
    headers ::= request.headers
    device_id_header := headers.single DEVICE_ID_HEADER
    sdk_version_header := headers.single SDK_VERSION_HEADER
    path := request.path

    // Handle identification requests before validation, as the caller doesn't know that information yet.
    if path == "/identify" and request.method == "GET":
      writer.write
          identity_payload id name address

    else if path == "/" or path.ends_with ".html" or path.ends_with ".css" or path.ends_with ".ico":
      handle_browser_request request writer

    // Validate device ID.
    else if device_id_header != id.stringify:
      logger.info "denied request, header: '$DEVICE_ID_HEADER' was '$device_id_header' not '$id'"
      writer.write_headers 403 --message="Device has id '$id', jag is trying to talk to '$device_id_header'"

    // Handle pings.
    else if path == "/ping" and request.method == "GET":
      writer.write
          json.encode {"status": "OK"}

    // Handle listing containers.
    else if path == "/list" and request.method == "GET":
      writer.write
          json.encode registry_.entries

    // Handle uninstalling containers.
    else if path == "/uninstall" and request.method == "PUT":
      defines_string ::= headers.single RUN_DEFINES_HEADER
      defines/Map := defines_string ? (json.parse defines_string) : {:}
      uninstall_image defines
      writer.write
          json.encode {"status": "OK"}

    // Handle firmware updates.
    else if path == "/firmware" and request.method == "PUT":
      install_firmware request.content_length request.body
      writer.write
          json.encode {"status": "OK"}
      // TODO(kasper): Maybe we can share the way we try to close down
      // the HTTP server nicely with the corresponding code where we
      // handle /code requests?
      writer.detach.close  // Close connection nicely before rebooting.
      sleep --ms=500
      esp32.deep_sleep (Duration --ms=10)

    // Validate SDK version before attempting to run code.
    else if sdk_version_header != vm_sdk_version:
      logger.info "denied request, header: '$SDK_VERSION_HEADER' was '$sdk_version_header' not '$vm_sdk_version'"
      writer.write_headers 406 --message="Device has $vm_sdk_version, jag has $sdk_version_header"

    // Handle code running.
    else if path == "/code" and request.method == "PUT":
      defines_string ::= headers.single RUN_DEFINES_HEADER
      defines/Map := defines_string ? (json.parse defines_string) : {:}
      install_image request.content_length request.body defines
      writer.write
          json.encode {"status": "OK"}
      if disabled:
        // TODO(kasper): There is no great way of closing down the HTTP server loop
        // and make sure we get a response delivered to all clients. For now, we
        // hope that sleeping for 0.5s is enough and then we simply cancel the task
        // responsible for running the loop.
        task::
          sleep --ms=500
          self.cancel
