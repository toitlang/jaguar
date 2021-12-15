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

HTTP_PORT ::= 9000
manager ::= ProgramManager
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
  task::
    identify id name address
  server := http.Server --logger=logger
  server.listen socket:: | request/http.Request writer/http.ResponseWriter |
    if request.path == "/code" and request.method == "PUT":
      install_program request.content_length request.body
      writer.write
        json.encode {"status": "success"}
    if request.path == "/ping" and request.method == "GET":
      writer.write
        json.encode {"status": "OK"}

install_program program_size/int reader/reader.Reader -> none:
  logger.debug "installing program with $program_size bytes"
  manager.new program_size
  written_size := 0
  image_reader := AlignedReader reader IMAGE_CHUNK_SIZE
  while data := image_reader.read:
    written_size += data.size
    manager.write data
  program := manager.commit
  logger.debug "installing program with $program_size bytes -> wrote $written_size bytes"

  gid ::= programs_registry_next_gid_
  logger.info "program $gid starting from $program"
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
