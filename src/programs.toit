// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import log
import uuid

IMAGE_WORD_SIZE ::= BYTES_PER_WORD
IMAGE_CHUNK_SIZE ::= (BITS_PER_WORD + 1) * IMAGE_WORD_SIZE

class ProgramManager:
  program_/Program? := null
  writer_/FlashImageWriter_? := null

  new image_size/int -> none:
    if program_:
      program_.kill
      program_ = null
    relocated_size := image_size - (image_size / IMAGE_CHUNK_SIZE) * IMAGE_WORD_SIZE
    writer_ = FlashImageWriter_ 0 relocated_size

  write bytes/ByteArray -> none:
    assert: writer_
    List.chunk_up 0 bytes.size IMAGE_CHUNK_SIZE: | from to |
      writer_.write bytes from to

  commit -> Program:
    assert: writer_
    // Commit the image to flash by writing the program header.
    id ::= uuid.NIL
    writer_.commit id.bytes_
    program_ = Program.internal_ writer_.offset writer_.size
    return program_

  close -> none:
    writer_.close
    writer_ = null

class Program:
  offset_ / int ::= 0
  size_   / int := 0

  /**
  Private constructor and state.
  */
  constructor.internal_ .offset_ .size_:
    // Do nothing.

  /**
  Whether this program is currently running.
  If a program isn't running, it will only be started with a call to $run.
  */
  is_running -> bool:
    return programs_registry_is_running_ offset_ size_

  /**
  Runs the program as a separate process in a new process group with
    the given group id.
  Returns the id of the newly spawned process.
  */
  run gid -> int:
    return programs_registry_spawn_ offset_ size_ gid

  // TODO(kasper): Make this non-polling.
  kill -> none:
    attempts := 0
    while programs_registry_is_running_ offset_ size_:
      result := programs_registry_kill_ offset_ size_
      if result: attempts++
      sleep --ms=10
    if attempts > 0: log.info "killed processes for" --tags={"program": "$this", "attempts": attempts}

  /**
  Returns a printable string representing the program.
  */
  stringify -> string:
    return "program:[$offset_,$(offset_ + size_)]"


// -------------------------------------------------------
// Implementation details.
// -------------------------------------------------------

class FlashImageWriter_:
  size    / int ::= ?
  offset  / int ::= ?
  content_      ::= ?

  constructor .offset .size:
    content_ = image_writer_create_ offset size

  write part/ByteArray from to -> none:
    image_writer_write_ content_ part from to

  commit id/ByteArray -> none:
    image_writer_commit_ content_ id

  close -> none:
    image_writer_close_ content_

programs_registry_next_gid_ -> int:
  #primitive.programs_registry.next_group_id

programs_registry_spawn_ offset size gid:
  #primitive.programs_registry.spawn

programs_registry_is_running_ offset size:
  #primitive.programs_registry.is_running

programs_registry_kill_ offset size:
  #primitive.programs_registry.kill

image_writer_create_ offset size:
  #primitive.image.writer_create

image_writer_write_ image part/ByteArray from/int to/int:
  #primitive.image.writer_write

image_writer_commit_ image id/ByteArray:
  #primitive.image.writer_commit

image_writer_close_ image:
  #primitive.image.writer_close
