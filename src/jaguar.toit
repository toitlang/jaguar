// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

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
monitor NetworkManager:
  network-endpoints_/int := 0
  network-is-disabled_/bool := false

  network-is-disabled -> bool:
    return network-is-disabled_

  start-network-endpoint -> none:
    await: not network-is-disabled_
    network-endpoints_++

  stop-network-endpoint -> none:
    network-endpoints_--

  disable-network -> none:
    network-is-disabled_ = true

  wait-for-network-down -> none:
    await: network-endpoints_ == 0

  enable-network -> none:
    network-is-disabled_ = false

  wait-for-request-to-disable-network -> none:
    await: network-is-disabled_

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
  blockers ::= []
  registry_.do: | name/string image/uuid.Uuid defines/Map? |
    start ::= Time.monotonic-us
    container := run-image image "started" name defines
    if defines.get JAG-NETWORK-DISABLED:
      timeout/Duration ::= compute-timeout defines --disabled
      blockers.add:: run-to-completion name container start timeout
  if blockers.is-empty: return
  // We have a number of containers that we need to allow
  // to run to completion before we return and let Jaguar
  // start serving requests.
  semaphore := monitor.Semaphore
  blockers.do: | lambda/Lambda |
    task::
      try:
        lambda.call
      finally:
        semaphore.up
  blockers.size.repeat: semaphore.down

serve device endpoints:
  network-user-count := 0
  lambdas := []
  for i := 0; i < endpoints.size; i++:
    endpoint/Endpoint := endpoints[i]
    if endpoint.uses-network: network-user-count++
    lambdas.add ::
      while true:
        attempts ::= 3
        failures := 0
        while failures < attempts:
          exception := null
          try:
            // Calling into the network-manager might block until we are allowed
            // to use the network.
            if endpoint.uses-network: network-manager.start-network-endpoint
            exception = catch:
              endpoint.run device
            // If we have a pending firmware upgrade, we take care of
            // it before trying to re-open the network.
            if firmware-is-upgrade-pending: firmware.upgrade
          finally:
            if endpoint.uses-network: network-manager.stop-network-endpoint

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
  if lambdas.size == 1:
    lambdas[0].call
  else:
    Task.group lambdas

validate-firmware:
  if firmware-is-validation-pending:
    if firmware.validate:
      logger.info "firmware update validated after connecting to network"
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
      id = uuid.parse arguments[1]
    else:
      id = config.get "id" --if-present=: uuid.parse it

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
        --id=id or uuid.NIL
        --name=name or "unknown"
        --port=port
        --chip=chip or "unknown"
        --config=config

flash-image image-size/int reader/reader.Reader name/string? defines/Map -> uuid.Uuid:
  with-timeout --ms=120_000: flash-mutex.do:
    image := registry_.install name defines:
      logger.debug "installing container image with $image-size bytes"
      written-size := 0
      writer := containers.ContainerImageWriter image-size
      while written-size < image-size:
        data := reader.read
        if not data: break
        // This is really subtle, but because the firmware writing crosses the RPC
        // boundary, the provided data might get neutered and handed over to another
        // process. In that case, the size after the call to writer.write is zero,
        // which isn't great for tracking progress. So we update the written size
        // before calling out to writer.write.
        written-size += data.size
        writer.write data
      logger.debug "installing container image with $image-size bytes -> wrote $written-size bytes"
      writer.commit --data=(name != null ? JAGUAR-INSTALLED-MAGIC : 0)
    return image
  unreachable

run-image image/uuid.Uuid cause/string name/string? defines/Map -> containers.Container:
  nick := name ? "container '$name'" : "program $image"
  suffix := defines.is-empty ? "" : " with $defines"
  logger.info "$nick $cause$suffix"
  return containers.start image

install-image image-size/int reader/reader.Reader name/string defines/Map -> none:
  image := flash-image image-size reader name defines
  if defines.get JAG-NETWORK-DISABLED:
    logger.info "container '$name' installed with $defines"
    logger.warn "container '$name' needs reboot to start with Jaguar disabled"
  else:
    timeout := compute-timeout defines --no-disabled
    if timeout: logger.warn "container '$name' needs 'jag.disabled' for 'jag.timeout' to take effect"
    run-image image "installed and started" name defines

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

run-to-completion name/string? container/containers.Container start/int timeout/Duration?:
  nick := name ? "container '$name'" : "program $container.id"

  // We're only interested in handling the timeout errors, so we
  // unwind and produce a stack trace in all other cases.
  filter ::= : it != DEADLINE-EXCEEDED-ERROR

  // Wait until the container is done or until we time out.
  code/int? := null
  catch --unwind=filter --trace=filter:
    with-timeout timeout: code = container.wait
  if not code:
    elapsed ::= Duration --us=Time.monotonic-us - start
    code = container.stop
    logger.info "$nick timed out after $elapsed"

  if code == 0:
    logger.info "$nick stopped"
  else:
    logger.error "$nick stopped - exit code $code"

run-code image-size/int reader/reader.Reader defines/Map -> none:
  network-disabled := (defines.get JAG-NETWORK-DISABLED) == true

  // Write the image into flash.
  image := flash-image image-size reader null defines

  if network-disabled: network-manager.disable-network
  timeout/Duration? := compute-timeout defines --disabled=network-disabled

  // We start the container from a separate task to allow the HTTP server
  // to continue operating. This also means that the container running
  // isn't covered by the flashing mutex or associated timeout.
  task::
    // First, we wait until we're ready to run the container. Usually,
    // we are ready right away, but if we've been asked to disable
    // Jaguar while running the container, we wait until the HTTP server
    // has been shut down and the network to be free.
    if network-disabled: network-manager.wait-for-network-down

    // Start the image and wait for it to complete.
    start ::= Time.monotonic-us
    container ::= run-image image "started" null defines
    run-to-completion null container start timeout

    // If Jaguar was disabled while running the container, now is the
    // time to restart the HTTP server.
    if network-disabled: network-manager.enable-network

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
