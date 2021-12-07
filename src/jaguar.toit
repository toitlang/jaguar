// Copyright (C) 2021 Toitware ApS.
// Use of this source code is governed by a Zero-Clause BSD license that can
// be found in the EXAMPLES_LICENSE file.

import http
import encoding.json
import net
import reader

import .programs
import .aligned_reader

PORT ::= 9000
manager ::= ProgramManager

main:
  network := net.open
  server := http.Server network
  print "Running shaguar on: $network.address:$PORT"
  server.listen PORT:: | request/http.Request writer/http.ResponseWriter |
    if request.path == "/code" and request.method == "PUT":
      install_program request.content_length request.body

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
