// Copyright (C) 2021 Toitware ApS.
// Use of this source code is governed by a Zero-Clause BSD license that can
// be found in the EXAMPLES_LICENSE file.

import http
import encoding.json
import net
import reader
import net.udp
import .programs
import .aligned_reader

IDENTIFY_PORT ::= 1990
IDENTIFY_ADDRESS ::= net.IpAddress.parse "255.255.255.255"

// TODO (jesper): Get mac address.
NAME ::= "Hest"

HTTP_PORT ::= 9000
manager ::= ProgramManager

main:
  network := net.open
  server := http.Server network
  addr := "http://$network.address:$HTTP_PORT"
  print "Running shaguar on: $addr"
  task::
    identify addr
  server.listen HTTP_PORT:: | request/http.Request writer/http.ResponseWriter |
    if request.path == "/code" and request.method == "PUT":
      install_program request.content_length request.body
      writer.write
        json.encode {"status": "success"}
    if request.path == "/ping" and request.method == "GET":
      writer.write
        json.encode {"status": "OK"}

install_program program_size/int reader/reader.Reader -> none:
  print "Installing program with size: $program_size"
  manager.new program_size
  length := 0
  image_reader := AlignedReader reader IMAGE_CHUNK_SIZE
  while data := image_reader.read:
    length += data.size
    manager.write data
  program := manager.commit
  print "Running program... WROOOM!!! written: $length/$program_size"
  program.run programs_registry_next_gid_

identify address/string -> none:
  network := net.open
  socket := network.udp_open
  socket.broadcast = true
  msg := udp.Datagram
    json.encode {
      "method": "jaguar.identify",
      "payload": {
        "name": NAME,
        "address": address,
      }
    }
    net.SocketAddress
      IDENTIFY_ADDRESS
      IDENTIFY_PORT

  while true:
    socket.send msg
    sleep --ms=200
