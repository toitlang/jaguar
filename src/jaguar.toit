// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import crypto.crc
import http
import log
import reader
import uuid
import monitor

import encoding.tison

import system
import system.assets
import system.containers
import system.firmware

import .container-registry
import .network
import .scheduled-callbacks
import .uart

interface Endpoint:
  run device/Device -> none
  name -> string
  uses-network -> bool

// Defines recognized by Jaguar for /run and /install requests.
JAG-NETWORK-DISABLED ::= "jag.network-disabled"
JAG-TIMEOUT  ::= "jag.timeout"

logger ::= log.Logger log.INFO-LEVEL log.DefaultTarget --name="jaguar"
flash-mutex ::= monitor.Mutex

firmware-is-validation-pending / bool := firmware.is-validation-pending
firmware-is-upgrade-pending / bool := false

/**
Jaguar can run containers while the network for Jaguar is disabled. You can
  enable this behavior by using `jag run -D jag.network-disabled ...` when
  starting the container. Use this mode to test how your apps behave
  when they run with no pre-established network.

We keep track of the state through the global $network-manager variable.
*/
class NetworkManager:
  signal_/monitor.Signal ::= monitor.Signal
  network-endpoints_/int := 0
  network-is-disabled_/bool := false

  serve device/Device endpoint/Endpoint -> none:
    uses-network := endpoint.uses-network
    if uses-network:
      signal_.wait: not network-is-disabled_
      network-endpoints_++
      signal_.raise
    try:
      endpoint.run device
    finally:
      if uses-network:
        network-endpoints_--
        signal_.raise

  network-is-disabled -> bool:
    return network-is-disabled_

  disable-network -> none:
    network-is-disabled_ = true
    signal_.raise

  wait-for-network-down -> none:
    signal_.wait: network-endpoints_ == 0

  enable-network -> none:
    network-is-disabled_ = false
    signal_.raise

  wait-for-request-to-disable-network -> none:
    signal_.wait: network-is-disabled_

network-manager / NetworkManager ::= NetworkManager

// The installed and named containers are kept in a registry backed
// by the flash (on the device).
registry_ / ContainerRegistry ::= ContainerRegistry

main arguments:
  device := Device.parse arguments
  endpoints := [
    EndpointHttp logger,
  ]
  uart := device.config.get "endpointUart"
  if uart: endpoints.add (EndpointUart --config=uart --logger=logger)
  main device endpoints

main device/Device endpoints/List:
  try:
    // We try to start all installed containers, but we catch any
    // exceptions that might occur from that to avoid blocking
    // the Jaguar functionality in case something is off.
    catch --trace: run-installed-containers
    // We are now ready to start Jaguar.
    serve device endpoints
  finally: | is-exception exception |
    // We shouldn't be able to get here without an exception having
    // been thrown, but we play it defensively and force an exception
    // if that should ever happen.
    if not is-exception: unreachable
    // Jaguar runs as a critical container, so an uncaught exception
    // will cause the system to reboot.
    logger.error "rebooting due to $exception.value"

run-installed-containers -> none:
  registry_.do: | name/string image/uuid.Uuid defines/Map? |
    run-image image "started" name defines

serve device/Device endpoints/List -> none:
  lambdas := endpoints.map: | endpoint/Endpoint | ::
    uses-network := endpoint.uses-network
    while true:
      attempts ::= 3
      failures := 0
      while failures < attempts:
        exception := catch:
          // If the endpoint needs network it might be blocked by the network
          // manager until the endpoint is allowed to use the network.
          network-manager.serve device endpoint

        if firmware-is-upgrade-pending: firmware.upgrade

        if endpoint.uses-network and network-manager.network-is-disabled:
          // If we were asked to shut down because the network was
          // disabled we may have gotten an exception. Ignore it.
          exception = null

        // Log exceptions and count the failures so we can back off
        // and avoid excessive attempts to re-open the network.
        if exception:
          failures++
          logger.warn "running Jaguar failed due to '$exception' ($failures/$attempts)"

      // If we need to validate the firmware and we've failed to do so
      // in the first round of attempts, we roll back to the previous
      // firmware right away.
      if firmware-is-validation-pending:
        logger.error "firmware update was rejected after failing to connect or validate"
        firmware.rollback
      backoff := Duration --s=5
      logger.info "backing off for $backoff"
      sleep backoff
  Task.group lambdas

validation-mutex/monitor.Mutex ::= monitor.Mutex
validate-firmware --reason/string -> none:
  validation-mutex.do:
    if firmware-is-validation-pending:
      if firmware.validate:
        logger.info "firmware update validated" --tags={"reason": reason}
        firmware-is-validation-pending = false
      else:
        logger.error "firmware update failed to validate"

class Device:
  id/uuid.Uuid
  name/string
  port/int
  chip/string
  config/Map

  constructor --.id --.name --.port --.chip --.config:

  static parse arguments -> Device:
    config := {:}
    if system.platform == system.PLATFORM-FREERTOS:
      assets.decode.get "config" --if-present=: | encoded |
        catch: config = tison.decode encoded

    id/uuid.Uuid? := null
    if arguments.size >= 2:
      id = uuid.Uuid.parse arguments[1]
    else:
      id = config.get "id" --if-present=: uuid.Uuid.parse it

    name/string? := null
    if arguments.size >= 3:
      name = arguments[2]
    else:
      name = config.get "name"

    port := HTTP-PORT
    if arguments.size >= 1:
      port = int.parse arguments[0]

    chip/string? := config.get "chip"

    return Device
        --id=id or uuid.Uuid.NIL
        --name=name or "unknown"
        --port=port
        --chip=chip or "unknown"
        --config=config

flash-image image-size/int reader/reader.Reader name/string? defines/Map --crc32/int -> uuid.Uuid:
  with-timeout --ms=120_000: flash-mutex.do:
    image := registry_.install name defines:
      logger.debug "installing container image with $image-size bytes"
      summer := crc.Crc.little-endian 32
          --polynomial=0xEDB88320
          --initial_state=0xffff_ffff
          --xor_result=0xffff_ffff
      written-size := 0
      writer := containers.ContainerImageWriter image-size
      while written-size < image-size:
        data := reader.read
        if not data: break
        summer.add data
        // This is really subtle, but because the firmware writing crosses the RPC
        // boundary, the provided data might get neutered and handed over to another
        // process. In that case, the size after the call to writer.write is zero,
        // which isn't great for tracking progress. So we update the written size
        // before calling out to writer.write.
        written-size += data.size
        writer.write data
      actual-crc32 := summer.get-as-int
      if actual-crc32 != crc32:
        logger.error "CRC32 mismatch."
        writer.close
        throw "CRC32 mismatch"
      logger.debug "installing container image with $image-size bytes -> wrote $written-size bytes"
      writer.commit --data=(name != null ? JAGUAR-INSTALLED-MAGIC : 0)
    return image
  unreachable

/**
Callbacks that are scheduled to run at a specific time.
This is used to kill containers that have deadlines.
*/
scheduled-callbacks := ScheduledCallbacks

run-image image/uuid.Uuid cause/string name/string? defines/Map -> none:
  network-disabled := (defines.get JAG-NETWORK-DISABLED) == true

  // First, we wait until we're ready to run the container. Usually,
  // we are ready right away, but if we've been asked to disable
  // Jaguar while running the container, we wait until the HTTP server
  // has been shut down and the network to be free.
  if network-disabled:
    if cause != "started":
      // In case this image was started by a network server give it time
      // to respond with an OK.
      sleep --ms=100
    network-manager.disable-network
    network-manager.wait-for-network-down

  timeout := compute-timeout defines --disabled=network-disabled
  start ::= Time.monotonic-us
  nick := name ? "container '$name'" : "program $image"
  suffix := defines.is-empty ? "" : " with $defines"
  logger.info "$nick $cause$suffix"

  container/containers.Container? := null

  // The token we get when registering a timeout callback.
  // Once the program has terminated we need to cancel the callback.
  cancelation-token := null

  container = containers.start image --on-stopped=:: | code/int |
    if cancelation-token:
      scheduled-callbacks.cancel cancelation-token

    if code == 0:
      logger.info "$nick stopped"
    else:
      logger.error "$nick stopped - exit code $code"

    // If Jaguar was disabled while running the container, now is the
    // time to restart the HTTP server.
    if network-disabled: network-manager.enable-network

  if timeout:
    // We schedule a callback to kill the container if it doesn't
    // stop on its own within the timeout.
    cancelation-token = scheduled-callbacks.schedule timeout::
      logger.error "$nick timed out after $timeout"
      container.stop

uninstall-image name/string -> none:
  with-timeout --ms=60_000: flash-mutex.do:
    if image := registry_.uninstall name:
      logger.info "container '$name' uninstalled"
    else:
      logger.error "container '$name' not found"

compute-timeout defines/Map --disabled/bool -> Duration?:
  jag-timeout := defines.get JAG-TIMEOUT
  if jag-timeout is int and jag-timeout > 0:
    return Duration --s=jag-timeout
  else if jag-timeout:
    logger.error "invalid $JAG-TIMEOUT setting ($jag-timeout)"
  return disabled ? (Duration --s=10) : null

install-firmware firmware-size/int reader/reader.Reader -> none:
  with-timeout --ms=300_000: flash-mutex.do:
    logger.info "installing firmware with $firmware-size bytes"
    written-size := 0
    writer := firmware.FirmwareWriter 0 firmware-size
    try:
      last := null
      while data := reader.read:
        written-size += data.size
        writer.write data
        percent := (written-size * 100) / firmware-size
        if percent != last:
          logger.info "installing firmware with $firmware-size bytes ($percent%)"
          last = percent
      writer.commit
      logger.info "installed firmware; ready to update on chip reset"
    finally:
      writer.close
