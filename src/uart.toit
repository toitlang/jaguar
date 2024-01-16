// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import binary show LITTLE-ENDIAN
import encoding.base64
import encoding.json
import encoding.ubjson
import gpio
import log
import uart
import reader show BufferedReader Reader
import system

import .jaguar

class EndpointUart implements Endpoint:
  config_/Map
  logger/log.Logger

  constructor --config/Map --logger/log.Logger:
    config_ = config
    this.logger = logger.with-name "uart"

  run device/Device -> none:
    logger.debug "starting endpoint"
    rx := gpio.Pin config_["rx"]
    port := uart.Port
        --rx=rx
        --tx=null
        --baud-rate=config_.get "baud-rate" --if-absent=: 115200

    try:
      client := UartClient --reader=port --writer=StdoutWriter --device=device
      client.run
    finally:
      port.close
      rx.close

  name -> string:
    return "uart"

interface UartWriter:
  write-framed data/ByteArray

/**
The stdout-writer expects its messages to be interleaved with
  normal logging and printing.
It uses a magic token to identify its messages, and expects the
  monitor to extract those.
*/
class StdoutWriter implements UartWriter:
  static MAGIC-TOKEN_ ::= "Jag15261520"

  write-framed data/ByteArray:
    // As of 2024-01 we can't write binary data to stdout.
    // Encode it as base64 and end with a newline.
    encoded := base64.encode data
    print_ "$MAGIC-TOKEN_$encoded"

class UartClient:
  /**
  The frame overhoad for packets received from the server.
  Two for the size bytes and one for the trailing '\n'.
  Does not include the command byte.
  */
  static FRAME-OVERHEAD_ ::= 3
  // Note that we send the magic number -1 for each byte, so that we
  // have less chance of accidentally detecting the magic number in
  // the data stream.
  static SYNC-MAGIC_ ::= #[27, 121, 55, 49, 253, 65, 123, 243]

  static COMMAND-SYNC_ ::= 0
  static COMMAND-PING_ ::= 1
  static COMMAND-IDENTIFY_ ::= 2
  static COMMAND-LIST-CONTAINERS_ ::= 3
  static COMMAND-UNINSTALL_ ::= 4
  static COMMAND-FIRMWARE_ ::= 5
  static COMMAND-INSTALL_ ::= 6
  static COMMAND-RUN_ ::= 7
  static COMMAND-UNKNOWN_ ::= 99

  static ACK-RESPONSE_ ::= 255

  reader/BufferedReader
  writer/UartWriter
  device/Device

  constructor --reader/Reader --.writer --.device:
    this.reader = BufferedReader reader

  run -> none:
    sync
    // We are synchronized. This means that something is listening on the other end.
    validate-firmware

    while true:
      size-bytes := reader.read-bytes 2
      size := LITTLE-ENDIAN.uint16 size-bytes 0
      data := reader.read-bytes size
      trailer := reader.read-byte
      if trailer != '\n':
        logger.error "trailer is not '\\n'" --tags={"trailer": trailer}
        // Try to align again by reading up to the next '\n'.
        reader.skip (reader.index-of '\n') + 1
        continue
      handle data

  /**
  Synchronizes with the server.

  Contrary to the rest of commands, synchronization looks at the stream directly, and
    skips any data that is not a sync request.

  Does not respond to any sync packet and keeps it in the stream for normal
    packet handling.
  */
  sync -> none:
    logger.debug "syncing"
    sync-payload-size := 3  + SYNC-MAGIC_.size // One byte the command. 2 bytes the sync-id.
    needed-packet-size := FRAME-OVERHEAD_ + sync-payload-size
    while true:
      // We are trying to align with the trailing '\n' of frames.
      index := reader.index-of '\n'
      if index + 1 != needed-packet-size:
        // Can't be a sync request.
        reader.skip (index + 1)
        continue
      packet := reader.bytes needed-packet-size
      pos := 0
      size := LITTLE-ENDIAN.uint16 packet pos
      pos += 2
      if size != sync-payload-size:
        continue
      if packet[pos++] != COMMAND-SYNC_:
        continue
      pos += 2  // Skip over sync-id.
      SYNC-MAGIC_.size.repeat: | i |
        // Note that we decrement the magic number.
        if packet[pos++] != SYNC-MAGIC_[i] - 1:
          continue
      // Found a sync packet.
      return

  handle request/ByteArray -> none:
    logger.debug "handling request" --tags={"request": request}
    command := request[0]
    data := request[1..]
    if command == COMMAND-SYNC_:
      handle-sync data
      return
    if command == COMMAND-PING_:
      handle-ping data
      return
    if command == COMMAND-IDENTIFY_:
      handle-identify data
      return
    if command == COMMAND-LIST-CONTAINERS_:
      handle-list-containers data
      return
    if command == COMMAND-UNINSTALL_:
      handle-uninstall data
      return
    if command == COMMAND-FIRMWARE_:
      handle-firmware data
      return
    if command == COMMAND-INSTALL_:
      handle-install data
      return
    if command == COMMAND-RUN_:
      handle-run data
      return
    send-response COMMAND-UNKNOWN_ #[]
    throw "Unknown command: $command"

  handle-sync data/ByteArray -> none:
    logger.debug "handle sync request"
    sync-id := LITTLE-ENDIAN.uint16 data 0
    send-response COMMAND-SYNC_  #[sync-id & 0xff, sync-id >> 8]

  handle-ping data/ByteArray -> none:
    logger.debug "handle ping request"
    send-response COMMAND-PING_ #[]
    return

  handle-identify data/ByteArray -> none:
    logger.debug "handle identify request"
    identity := {
      "name": "$device.name",
      "id": "$device.id",
      "chip": "$device.chip",
      "sdkVersion": "$system.vm-sdk-version",
    }
    encoded := ubjson.encode identity
    send-response COMMAND-IDENTIFY_ encoded
    return

  handle-list-containers data/ByteArray -> none:
    result := ubjson.encode registry_.entries
    send-response COMMAND-LIST-CONTAINERS_ result

  handle-uninstall data/ByteArray -> none:
    logger.debug "handle uninstall request"
    id := data.to-string
    uninstall-image id
    send-response COMMAND-UNINSTALL_ #[]
    return

  handle-firmware data/ByteArray -> none:
    logger.debug "handle firmware request"
    firmware-size := LITTLE-ENDIAN.uint32 data 0
    acking-reader := AckingReader firmware-size reader --send-ack=(:: send-ack it)
    // Signal that we are ready to receive the firmware.
    send-response COMMAND-FIRMWARE_ #[]
    install-firmware firmware-size acking-reader

  handle-install data/ByteArray -> none:
    logger.debug "handle install request"
    pos := 0
    container-size := LITTLE-ENDIAN.uint32 data pos
    pos += 4
    container-id-size := LITTLE-ENDIAN.uint16 data pos
    pos += 2
    container-id := data[pos..pos + container-id-size].to-string
    pos += container-id-size
    encoded-defines := data[pos..]
    defines := ubjson.decode encoded-defines
    acking-reader := AckingReader container-size reader --send-ack=(:: send-ack it)
    // Signal that we are ready to receive the container.
    send-response COMMAND-INSTALL_ #[]
    install-image container-size acking-reader container-id defines

  handle-run data/ByteArray -> none:
    image-size := LITTLE-ENDIAN.uint32 data 0
    encoded-defines := data[4..]
    defines := ubjson.decode encoded-defines
    acking-reader := AckingReader image-size reader --send-ack=(:: send-ack it)
    // Signal that we are ready to receive the image.
    send-response COMMAND-RUN_ #[]
    run-code image-size acking-reader defines

  send-response command/int response/ByteArray -> none:
    data := #[command] + response
    if data.size > 65535:
      throw "response too large"
    size-bytes := ByteArray 2
    LITTLE-ENDIAN.put-uint16 size-bytes 0 data.size
    send size-bytes + data + #['\n']

  send data/ByteArray -> none:
    writer.write-framed data

  send-ack consumed/int -> none:
    bytes := ByteArray 3
    bytes[0] = ACK-RESPONSE_
    LITTLE-ENDIAN.put-uint16 bytes 1 consumed
    send bytes

class AckingReader implements Reader:
  size_/int
  wrapped-reader_/Reader
  send-ack_/Lambda
  produced_/int := 0

  constructor .size_ .wrapped-reader_ --send-ack/Lambda:
    send-ack_ = send-ack

  read -> ByteArray?:
    if produced_ == size_:
      return null
    data := wrapped-reader_.read
    if data == null:
      send-ack_.call 0
      return null
    produced_ += data.size
    send-ack_.call data.size
    return data
