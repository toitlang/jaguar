// Copyright (C) 2021 Toitware ApS.
// Use of this source code is governed by a Zero-Clause BSD license that can
// be found in the EXAMPLES_LICENSE file.

import core.message_manual_decoding_ show print_for_manually_decoding_

import .jaguar

install_system_message_handler:
  handler := MessageHandler
  set_system_message_handler_ SYSTEM_TERMINATED_ handler
  set_system_message_handler_ SYSTEM_MIRROR_MESSAGE_ handler

class MessageHandler implements SystemMessageHandler_:
  on_message type gid pid args:
    if type == SYSTEM_MIRROR_MESSAGE_:
      // TODO(kasper): Jag needs to help here and make it easy
      // to decode the stack traces.
      print_for_manually_decoding_ args
    else if type == SYSTEM_TERMINATED_:
      value := args
      logger.info "program $gid terminated with exit code $value"
