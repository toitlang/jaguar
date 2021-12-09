// Copyright (C) 2021 Toitware ApS.
// Use of this source code is governed by a Zero-Clause BSD license that can
// be found in the EXAMPLES_LICENSE file.

import encoding.base64 as base64
import log

install_system_message_handler logger/log.Logger:
  handler := MessageHandler logger
  set_system_message_handler_ SYSTEM_TERMINATED_ handler
  set_system_message_handler_ SYSTEM_MIRROR_MESSAGE_ handler

class MessageHandler implements SystemMessageHandler_:

  logger/log.Logger

  constructor .logger:
    logger

  on_message type gid pid args:
    if type == SYSTEM_MIRROR_MESSAGE_:
      // TODO(kasper): Jag needs to help here and make it easy
      // to decode the stack traces.
      print_manual_decode args --system=gid==0
    else if type == SYSTEM_TERMINATED_:
      value := args
      logger.info "program $gid terminated with exit code $value"

  print_manual_decode message/ByteArray --system/bool --from=0 --to=message.size:
    // Print a message on output so that that you can easily decode.
    print_ "----"
    print_ "Decode system message with:"
    print_ "----"
    // Block size must be a multiple of 3 for this to work, due to the 3/4 nature
    // of base64 encoding.
    BLOCK_SIZE := 1500
    for i := from; i < to; i += BLOCK_SIZE:
      end := i >= to - BLOCK_SIZE
      prefix := i == from ? "jag decode $(system ? "--system " : "")" : ""
      base64_text := base64.encode (message.copy i (end ? to : i + BLOCK_SIZE))
      postfix := end ? "" : "\\"
      print_ "$prefix$base64_text$postfix"
