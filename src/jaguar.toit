// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.json
import http
import log
import net
import net.udp
import reader
import esp32
import uuid

import .aligned_reader
import .programs
import .system_message_handler

IDENTIFY_PORT ::= 1990
IDENTIFY_ADDRESS ::= net.IpAddress.parse "255.255.255.255"
DEVICE_ID_HEADER ::= "X-Jaguar-Device-ID"
SDK_VERSION_HEADER ::= "X-Jaguar-SDK-Version"

HTTP_PORT ::= 9000
manager ::= ProgramManager --logger=logger
logger ::= log.Logger log.INFO_LEVEL log.DefaultTarget

main args:
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

  install_system_message_handler logger
  network := net.open
  socket := network.tcp_listen port
  address := "http://$network.address:$socket.local_address.port"
  logger.info "Running Jaguar device '$name' (id: '$id') on '$address'"

  exception := catch --trace:
    last := manager.last
    if last:
      gid ::= programs_registry_next_gid_
      logger.info "Program $gid re-starting from $last"
      last.run gid
  if exception:
    // Don't keep trying to run malformed programs.
    manager.last = null

  task::
    identify id name address
  server := http.Server --logger=logger
  server.listen socket:: | request/http.Request writer/http.ResponseWriter |
    device_id_header := request.headers.single DEVICE_ID_HEADER
    sdk_version_header := request.headers.single SDK_VERSION_HEADER

    // Validate device ID
    if device_id_header != id.stringify:
      logger.info "Denied request, header: '$DEVICE_ID_HEADER' was '$device_id_header' not '$id'"
      writer.write_headers 403 --message="Device has id '$id', jag is trying to talk to '$device_id_header'"

    // Validate SDK version
    else if sdk_version_header != vm_sdk_version:
      logger.info "Denied request, header: '$SDK_VERSION_HEADER' was '$sdk_version_header' not '$vm_sdk_version'"
      writer.write_headers 406 --message="Device has $vm_sdk_version, jag has $sdk_version_header"

    else if request.path == "/code" and request.method == "PUT":
      install_program request.content_length request.body
      writer.write
        json.encode {"status": "success"}

    else if request.path == "/ping" and request.method == "GET":
      writer.write
        json.encode {"status": "OK"}

install_program program_size/int reader/reader.Reader -> none:
  logger.debug "Installing program with $program_size bytes"
  manager.new program_size
  written_size := 0
  image_reader := AlignedReader reader IMAGE_CHUNK_SIZE
  while data := image_reader.read:
    written_size += data.size
    manager.write data
  program := manager.commit
  logger.debug "Installing program with $program_size bytes -> wrote $written_size bytes"

  gid ::= programs_registry_next_gid_
  logger.info "Program $gid starting from $program"
  program.run gid

identify id/uuid.Uuid name/string address/string -> none:
  network := net.open
  socket := network.udp_open
  socket.broadcast = true
  msg := udp.Datagram
    json.encode {
      "method": "jaguar.identify",
      "payload": {
        "name": name,
        "id": id.stringify,
        "address": address,
        "wordSize": BYTES_PER_WORD,
      }
    }
    net.SocketAddress
      IDENTIFY_ADDRESS
      IDENTIFY_PORT

  while true:
    socket.send msg
    sleep --ms=200
