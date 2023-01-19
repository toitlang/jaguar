// Copyright (C) 2023 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import http
import http.connection as http
import log
import net.tcp

// This is a specialized HTTP server that only handles one request
// at a time. It would be ideal if the $http.Server.listen supported
// specifying the maximum concurrency in which case this code would
// be a specialization with a maximum concurrency of one.
class JaguarServer extends http.Server:
  constructor --logger/log.Logger:
    super --logger=logger

  listen server_socket/tcp.ServerSocket handler/Lambda -> none:
    while true:
      socket := server_socket.accept
      if not socket: continue
      connection := http.Connection socket

      detached := false
      try:
        address := socket.peer_address
        logger := logger_.with_tag "peer" address
        logger.debug "client connected"
        e := catch:
          detached = run_connection_ connection handler logger
        close_logger := e ? (logger.with_tag "reason" e) : logger
        if detached:
          close_logger.debug "client socket detached"
        else:
          close_logger.debug "client disconnected"
      finally:
        if not detached: socket.close
