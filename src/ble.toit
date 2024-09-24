// Copyright (C) 2024 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import encoding.base64
import encoding.json
import encoding.ubjson
import io
import io show LITTLE-ENDIAN
import log
import ble
import monitor
import system

import .jaguar

class EndpointBle implements Endpoint:
  config_/Map
  logger/log.Logger

  constructor --config/Map --logger/log.Logger:
    config_ = config
    this.logger = logger.with-name "ble"

  run device/Device -> none:
    logger.debug "starting endpoint"

    client := BleClient --device=device --logger=logger
    try:
      client.run
    finally:
      client.close

  name -> string:
    return "uart"

interface UartWriter:
  write-framed data/ByteArray

SERVICE-UUID ::= #[0x0c, 0xfb, 0x6d, 0x88, 0xd8, 0x65, 0x41, 0xc4,
                   0xa7, 0xa9, 0x49, 0x86, 0xae, 0x5c, 0xb6, 0x4c]

DESCRIPTOR-UUID ::= #[0x00, 0x00, 0x29, 0x01, 0x00, 0x00, 0x10, 0x00,
                      0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb]

class BleClient:
  static UUID-BASE_ ::= 0xff

  static COMMAND-PING_ ::= 1
  static COMMAND-IDENTIFY_ ::= 2
  static COMMAND-LIST-CONTAINERS_ ::= 3
  static COMMAND-UNINSTALL_ ::= 4
  static COMMAND-START-UPLOAD_ ::= 5
  static COMMAND-UPLOAD_ ::= 6

  adapter/ble.Adapter? := null
  peripheral/ble.Peripheral? := null
  service/ble.LocalService? := null
  ble-endpoints_/List? := null

  device/Device
  logger/log.Logger

  static characteristic-id id/int -> ble.BleUuid:
    uuid := SERVICE-UUID.copy
    uuid[2] = 0xFF
    uuid[3] = id
    return ble.BleUuid uuid

  constructor --.device --logger/log.Logger:
    this.logger = logger.with-name "ble"

  run:
    logger.info "starting ble endpoint"
    adapter = ble.Adapter
    peripheral = adapter.peripheral
    service = peripheral.add-service (ble.BleUuid SERVICE-UUID)
    upload := UploadBleEndpoint service --logger=logger
    start-upload := StartUploadBleEndpoint device service --logger=logger --upload=upload
    ble-endpoints_ = [
      PingBleEndpoint service --logger=logger,
      IdentifyBleEndpoint device service --logger=logger,
      ListContainersBleEndpoint device service --logger=logger,
      UninstallBleEndpoint device service --logger=logger,
      start-upload,
      upload,
    ]

    peripheral.deploy

    company-id := #[0xff, 0xff]
    jaguar-token := #['J', 'a', 'g', '-', 0x70, 0x17]
    max-name-length := 31 - 2 - (2 + company-id.size + jaguar-token.size)
    shortened-name := device.name
    if shortened-name.size > max-name-length:
      shortened-name = shortened-name[..max-name-length]
    peripheral.start-advertise
        ble.AdvertisementData
            --name=shortened-name
            --manufacturer-data=company-id + jaguar-token
            --flags=ble.BLE-ADVERTISE-FLAGS-GENERAL-DISCOVERY |
                    ble.BLE-ADVERTISE-FLAGS-BREDR-UNSUPPORTED
        --interval=Duration --ms=160
        --connection-mode=ble.BLE-CONNECT-MODE-UNDIRECTIONAL

    // Keep running.
    // The only way to stop is to cancel the task.
    latch := monitor.Latch
    latch.get

  close:
    logger.debug "closing ble client"
    critical-do:
      if ble-endpoints_:
        ble-endpoints_.do: | service/BleEndpoint | service.close
        ble-endpoints_ = null
      if peripheral:
        peripheral.close
        peripheral = null
      if adapter:
        adapter.close
        adapter = null

abstract class BleEndpoint:
  static WRITE-TIMEOUT-MS_ ::= 10 * 1000

  characteristic_/ble.LocalCharacteristic
  logger/log.Logger

  /**
  Constructs a new RPC characteristic.

  The characteristic UUID is constructed from the service UUID by replacing
    the third byte with 0xff and the 4th with the id.
  */
  constructor
      service/ble.LocalService
      id/int
      --.logger
      --properties/int
      --permissions/int
      --value/io.Data?=null:
    uuid := service.uuid.to-byte-array.copy
    uuid[2] = BleClient.UUID-BASE_
    uuid[3] = id

    characteristic_ = service.add-characteristic
        ble.BleUuid uuid
        --properties=properties
        --permissions=permissions
        --value=value
        // TODO(florian): move the timeout to the run_ below.
        --read-timeout-ms=WRITE-TIMEOUT-MS_

  close -> none:

/**
An RPC service (not to be confused with a BLE service) is built
  on top of BLE characteristics.

A BLE device writes a value to the characteristic, and the device sets
  the value of the characteristic to the response.
*/
abstract class TaskBleEndpoint extends BleEndpoint:
  static WRITE-TIMEOUT-MS_ ::= 10 * 1000

  task_/Task? := null

  /**
  Constructs a new RPC characteristic.

  The characteristic UUID is constructed from the service UUID by replacing
    the third byte with 0xff and the 4th with the id.
  */
  constructor
      service/ble.LocalService
      id/int
      --logger/log.Logger
      --properties/int
      --permissions/int
      --value/io.Data?=null:

    super service id
        --logger=logger
        --properties=properties
        --permissions=permissions
        --value=value

    task_ = task:: run_

  close -> none:
    critical-do:
      if task_:
        task_.cancel
        task_ = null

  abstract run_ -> none

abstract class RpcBleEndpoint extends TaskBleEndpoint:
  static PROPERTIES_ ::= ble.CHARACTERISTIC-PROPERTY-READ | ble.CHARACTERISTIC-PROPERTY-WRITE
  static PERMISSIONS_ ::= ble.CHARACTERISTIC-PERMISSION-READ | ble.CHARACTERISTIC-PERMISSION-WRITE

  constructor
      service/ble.LocalService
      id/int
      --logger/log.Logger:
    super service id
        --logger=logger
        --properties=PROPERTIES_
        --permissions=PERMISSIONS_

  abstract handle-request data/ByteArray -> io.Data

  run_ -> none:
    // TODO(florian): Move the timeout to here, once Jaguar uses a newer SDK.
    // characteristic_.handle-write-request --timeout-ms=WRITE_TIMEOUT_MS_: | data/ByteArray |
    characteristic_.handle-write-request: | data/ByteArray |
      response := handle-request data
      characteristic_.set-value response

class PingBleEndpoint extends RpcBleEndpoint:
  constructor service/ble.LocalService --logger/log.Logger:
    super service BleClient.COMMAND-PING_ --logger=logger

  handle-request data/ByteArray -> ByteArray:
    return data

class IdentifyBleEndpoint extends BleEndpoint:
  device/Device

  constructor .device service/ble.LocalService --logger/log.Logger:
    identity-payload := ubjson.encode {
      "name": device.name,
      "id": "$device.id",
      "chip": device.chip,
      "sdkVersion": system.vm-sdk-version,
    }
    super service BleClient.COMMAND-IDENTIFY_
        --logger=logger
        --properties=ble.CHARACTERISTIC-PROPERTY-READ
        --permissions=ble.CHARACTERISTIC-PERMISSION-READ
        --value=identity-payload

class ListContainersBleEndpoint extends RpcBleEndpoint:
  device/Device

  constructor .device service/ble.LocalService --logger/log.Logger:
    super service BleClient.COMMAND-LIST-CONTAINERS_ --logger=logger

  handle-request data/ByteArray -> ByteArray:
    logger.debug "list containers" --tags={"data": data}
    // In order to keep the responses small, we require the client to request
    // each individual container. A request of #[0xff, 0xff] returns the number
    // of containers.
    if data.size != 2:
      logger.error "list containers request has wrong size" --tags={"data": data}
      return #[]

    if data[0] == 0xff and data[1] == 0xff:
      result := ByteArray 2
      LITTLE-ENDIAN.put-uint16 result 0 registry_.entries.size
      return result
    entry-id := LITTLE-ENDIAN.uint16 data 0
    if entry-id >= registry_.entries.size:
      logger.error "list containers request out of bounds" --tags={"data": data}
      return #[]
    entries := registry_.entries
    keys := entries.keys
    id := keys[entry-id]
    return ubjson.encode [id, entries[id]]

class UninstallBleEndpoint extends TaskBleEndpoint:
  device/Device

  constructor .device service/ble.LocalService --logger/log.Logger:
    super service BleClient.COMMAND-UNINSTALL_
        --logger=logger
        --properties=ble.CHARACTERISTIC-PROPERTY-WRITE
        --permissions=ble.CHARACTERISTIC-PERMISSION-WRITE

  run_ -> none:
    while true:
      data := characteristic_.read
      name := data.to-string
      logger.debug "uninstall" --tags={"name": name}
      uninstall-image name

class BleReader extends io.Reader:
  static READ-TIMEOUT-MS_ ::= 5 * 1000

  byte-size/int
  endpoint_/UploadBleEndpoint
  total-handled_/int := 0

  constructor .byte-size .endpoint_:

  read_ -> ByteArray?:
    if total-handled_ >= byte-size:
      return null
    with-timeout --ms=READ-TIMEOUT-MS_:
      return endpoint_.read-at-most (byte-size - total-handled_)
    unreachable

  is-done -> bool:
    return total-handled_ >= byte-size

class UploadBleEndpoint extends BleEndpoint:
  received-count_/int := 0
  pending_/ByteArray := #[]

  constructor
      service/ble.LocalService
      --logger/log.Logger:
    super service BleClient.COMMAND-UPLOAD_
        --logger=logger
        --properties=ble.CHARACTERISTIC-PROPERTY-READ | ble.CHARACTERISTIC-PROPERTY-WRITE
        --permissions=ble.CHARACTERISTIC-PERMISSION-READ | ble.CHARACTERISTIC-PERMISSION-WRITE
        --value=#[0x00, 0x00, 0x00, 0x00]

  reset-upload -> none:
    received-count_ = 0
    pending_ = #[]

  get-more -> none:
    chunk := characteristic_.read
    pending_ += chunk
    received-count_ += chunk.size
    received-bytes := ByteArray 4
    LITTLE-ENDIAN.put-uint32 received-bytes 0 received-count_
    characteristic_.set-value received-bytes

  read-uint32 -> int:
    while pending_.size < 4:
      get-more
    result := LITTLE-ENDIAN.uint32 pending_ 0
    pending_ = pending_[4..]
    return result

  read-uint16 -> int:
    while pending_.size < 2:
      get-more
    result := LITTLE-ENDIAN.uint16 pending_ 0
    pending_ = pending_[2..]
    return result

  read-string n/int -> string:
    while pending_.size < n:
      get-more
    result := pending_[..n].to-string
    pending_ = pending_[n..]
    return result

  read-at-most n/int -> ByteArray:
    if pending_.is-empty:
      get-more

    if pending_.size < n:
      result := pending_
      pending_ = #[]
      return result
    result := pending_[..n]
    pending_ = pending_[n..]
    return result

  read n/int -> ByteArray:
    result := #[]
    while result.size < n:
      chunk := read-at-most n - result.size
      result += chunk
    return result

class StartUploadBleEndpoint extends RpcBleEndpoint:
  static KIND-INSTALL_ ::= 0
  static KIND-RUN_ ::= 1

  static RETURN-CODE-OK_ ::= 0
  static RETURN-CODE-ERROR_ ::= 1
  static RETURN-CODE-SDK-MISMATCH_ ::= 2
  static RETURN-CODE-UNKNOWN-KIND_ ::= 3

  device/Device
  upload_/UploadBleEndpoint
  upload-task_/Task? := null

  constructor .device service/ble.LocalService --logger/log.Logger --upload/UploadBleEndpoint:
    upload_ = upload
    super service BleClient.COMMAND-START-UPLOAD_ --logger=logger

  in-task_ tags/Map callback/Lambda -> none:
    if upload-task_:
      logger.warn "existing upload canceled"
      upload-task_.cancel
    upload-task_ = task::
      e := catch:
        try:
          logger.debug "starting upload" --tags=tags
          callback.call
          logger.debug "upload done"
        finally:
          upload-task_ = null
      if e:
        logger.error "upload failed" --tags={"error": "$e"}

  check-sdk-version_ payload/ByteArray offset/int -> int:
    sdk-version-size := LITTLE-ENDIAN.uint16 payload offset
    offset += 2
    sdk-version := payload[offset..offset + sdk-version-size].to-string
    offset += sdk-version-size
    if sdk-version != system.vm-sdk-version:
      logger.error "sdk version mismatch" --tags={"expected": system.vm-sdk-version, "actual": sdk-version}
      return -1
    return offset

  start-install_ payload/ByteArray offset/int -> int:
    offset = check-sdk-version_ payload offset
    if offset == -1:
      return RETURN-CODE-SDK-MISMATCH_
    container-size := LITTLE-ENDIAN.uint32 payload offset
    offset += 4
    crc32 := LITTLE-ENDIAN.uint32 payload offset
    offset += 4
    container-name-size := LITTLE-ENDIAN.uint16 payload offset
    offset += 2
    container-name := payload[offset..offset + container-name-size].to-string
    offset += container-name-size
    encoded-defines := payload[offset..]
    defines := ubjson.decode encoded-defines
    in-task_ {"kind": "install", "size": container-size, "name": container-name}::
      reader := BleReader container-size upload_
      install-image container-size reader container-name defines --crc32=crc32
    return RETURN-CODE-OK_

  start-run_ payload/ByteArray offset/int -> int:
    offset = check-sdk-version_ payload 1
    if offset == -1:
      return RETURN-CODE-SDK-MISMATCH_
    image-size := LITTLE-ENDIAN.uint32 payload offset
    offset += 4
    crc32 := LITTLE-ENDIAN.uint32 payload offset
    offset += 4
    encoded-defines := payload[offset..]
    defines := ubjson.decode encoded-defines
    in-task_ {"kind": "run", "image-size": image-size}::
      reader := BleReader image-size upload_
      run-code image-size reader defines --crc32=crc32
    return RETURN-CODE-OK_

  handle-request command/ByteArray -> io.Data:
    // We assume that we always get full commands.
    e := catch:
      kind := command[0]
      logger.debug "received command" --tags={"kind": kind}
      return-code/int := ?
      if kind == KIND-INSTALL_:
        return-code = start-install_ command 1
      else if kind == KIND-RUN_:
        return-code = start-run_ command 1
      else:
        logger.error "unknown command" --tags={"kind": kind}
        return-code = RETURN-CODE-UNKNOWN-KIND_
    if e:
      logger.error "error handling command" --tags={"error": "$e"}
      return #[RETURN-CODE-ERROR_]
    return #[RETURN-CODE-OK_]
