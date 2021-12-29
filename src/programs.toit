// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import device
import log
import uuid

IMAGE_WORD_SIZE  ::= BYTES_PER_WORD
IMAGE_CHUNK_SIZE ::= (BITS_PER_WORD + 1) * IMAGE_WORD_SIZE
IMAGE_PAGE_SIZE  ::= BYTES_PER_WORD == 4 ? 4 * KB : 32 * KB

// Build a string key that is short enough to be used by device.FlashStore
// and that is bound to the Toit SDK version, so we can try to avoid
// finding stale information about programs in flash.
JAGUAR_LAST_PROGRAM_ ::=
    (uuid.uuid5 "jaguar.programs.last" vm_sdk_version).stringify.copy 0 13

class ProgramManager:
  program_/Program? := null
  writer_/FlashImageWriter_? := null
  logger_/log.Logger
  store_/device.FlashStore ::= device.FlashStore

  next_offset_/int := 0

  constructor --logger=log.default:
    logger_ = logger

  last -> Program?:
    stored ::= store_.get JAGUAR_LAST_PROGRAM_
    if not stored: return null
    return Program.from_map stored logger_

  last= program/Program? -> none:
    if program:
      store_.set JAGUAR_LAST_PROGRAM_ program.to_map
    else:
      store_.delete JAGUAR_LAST_PROGRAM_

  new image_size/int -> none:
    last = null
    if program_:
      program_.kill
      program_ = null
    relocated_size := image_size - (image_size / IMAGE_CHUNK_SIZE) * IMAGE_WORD_SIZE
    2.repeat:
      catch --trace=(: it != "OUT_OF_BOUNDS"):
        writer_ = FlashImageWriter_ next_offset_ relocated_size
        allocated_size ::= (relocated_size + IMAGE_PAGE_SIZE - 1) & (-IMAGE_PAGE_SIZE)
        next_offset_ += allocated_size
        return
      // Start over from offset zero.
      next_offset_ = 0

  write bytes/ByteArray -> none:
    assert: writer_
    List.chunk_up 0 bytes.size IMAGE_CHUNK_SIZE: | from to |
      writer_.write bytes from to

  commit -> Program:
    assert: writer_
    // Commit the image to flash by writing the program header.
    id ::= uuid.NIL
    writer_.commit id.bytes_
    program_ = Program.internal_ writer_.offset writer_.size logger_
    last = program_
    return program_

  close -> none:
    writer_.close
    writer_ = null

class Program:
  offset_ / int ::= 0
  size_   / int := 0
  logger_ / log.Logger

  /**
  Private constructor and state.
  */
  constructor.internal_ .offset_ .size_ .logger_:
    // Do nothing.

  constructor.from_map map/Map .logger_:
    offset_ = map["offset"]
    size_ = map["size"]

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

  /**
  Kills the program and makes sure it does not run.
  */
  kill -> none:
    attempts := 0
    while programs_registry_is_running_ offset_ size_:
      result := programs_registry_kill_ offset_ size_
      if result: attempts++
      sleep --ms=10
    if attempts > 0:
      logger_.debug "program killed" --tags={"program": "$this", "attempts": attempts}

  /**
  Returns a printable string representing the program.
  */
  stringify -> string:
    return "flash @ [$offset_,$(offset_ + size_)]"

  /**
  Returns a map representation of the program.
  */
  to_map -> Map:
    return {"offset": offset_, "size": size_}

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
