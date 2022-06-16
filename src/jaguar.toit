// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json
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

IDENTIFY_PORT ::= 1990
IDENTIFY_ADDRESS ::= net.IpAddress.parse "255.255.255.255"
DEVICE_ID_HEADER ::= "X-Jaguar-Device-ID"
SDK_VERSION_HEADER ::= "X-Jaguar-SDK-Version"

HTTP_PORT ::= 9000
logger ::= log.Logger log.INFO_LEVEL log.DefaultTarget --name="jaguar"

main args:
  try:
    exception := catch --trace: serve args
    logger.error "rebooting due to $(exception)"
  finally:
    esp32.deep_sleep (Duration --s=1)

serve args:
  port := HTTP_PORT
  if args.size >= 1:
    port = int.parse args[0]

  image_config := {:}
  if platform == PLATFORM_FREERTOS:
    image_config = esp32.image_config or {:}

  id/uuid.Uuid := uuid.NIL
  if args.size >= 2:
    id = uuid.parse args[1]
  else:
    id = image_config.get "id"
      --if_absent=: id
      --if_present=: uuid.parse it

  name/string := "unknown"
  if args.size >= 3:
    name = args[2]
  else:
    name = image_config.get "name" --if_absent=: name

  while true:
    failures := 0
    attempts := 3
    while failures < attempts:
      exception := catch: run id name port
      if not exception: continue
      failures++
      logger.warn "running Jaguar failed due to '$exception' ($failures/$attempts)"
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

    // We run two tasks concurrently: One broadcasts the device identity
    // via UDP and one serves incoming HTTP requests. If one of the tasks
    // fail, we take the other one down to clean up nicely.
    done := monitor.Semaphore
    server_task = task::
      try:
        error = catch: serve_incoming_requests socket id
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

install_program program_size/int reader/reader.Reader -> none:
  with_timeout --ms=60_000: install_mutex.do:
    // Uninstall everything but Jaguar.
    images := containers.images
    jaguar := containers.current
    images.do: | id/uuid.Uuid |
      if id != jaguar:
        containers.uninstall id

    logger.debug "installing program with $program_size bytes"
    written_size := 0
    writer := containers.ContainerImageWriter program_size
    while data := reader.read:
      written_size += data.size
      writer.write data
    program := writer.commit

    logger.debug "installing program with $program_size bytes -> wrote $written_size bytes"
    logger.info "starting program $program"
    containers.start program

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

broadcast_identity network/net.Interface id/uuid.Uuid name/string address/string -> none:
  payload ::= json.encode {
    "method": "jaguar.identify",
    "payload": {
      "name": name,
      "id": id.stringify,
      "sdkVersion": vm_sdk_version,
      "address": address,
      "wordSize": BYTES_PER_WORD,
    }
  }
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

serve_incoming_requests socket/tcp.ServerSocket id/uuid.Uuid:
  server := http.Server --logger=logger
  server.listen socket:: | request/http.Request writer/http.ResponseWriter |
    device_id_header := request.headers.single DEVICE_ID_HEADER
    sdk_version_header := request.headers.single SDK_VERSION_HEADER

    // Validate device ID.
    if device_id_header != id.stringify:
      logger.info "denied request, header: '$DEVICE_ID_HEADER' was '$device_id_header' not '$id'"
      writer.write_headers 403 --message="Device has id '$id', jag is trying to talk to '$device_id_header'"

    // Handle pings.
    else if request.path == "/ping" and request.method == "GET":
      writer.write
          json.encode {"status": "OK"}

    // Handle firmware updates.
    else if request.path == "/firmware" and request.method == "PUT":
      install_firmware request.content_length request.body
      writer.write
          json.encode {"status": "OK"}
      writer.detach.close  // Close connection nicely before rebooting.
      sleep --ms=500
      esp32.deep_sleep (Duration --ms=10)

    // Validate SDK version before attempting to run code.
    else if sdk_version_header != vm_sdk_version:
      logger.info "denied request, header: '$SDK_VERSION_HEADER' was '$sdk_version_header' not '$vm_sdk_version'"
      writer.write_headers 406 --message="Device has $vm_sdk_version, jag has $sdk_version_header"

    // Handle code running.
    else if request.path == "/code" and request.method == "PUT":
      install_program request.content_length request.body
      writer.write
          json.encode {"status": "OK"}
