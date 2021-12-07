// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

import reader

class AlignedReader implements reader.Reader:
  buffer_/reader.BufferedReader
  alignment_/int
  max_size_/int

  constructor input/reader.Reader .alignment_ --max_size/int=1024:
    buffer_ = reader.BufferedReader input
    max_size_ = max_size - max_size % alignment_

  read -> ByteArray?:
    while true:
      // Check if at end.
      if not buffer_.can_ensure 1: return null

      buffered := buffer_.buffered
      // Try to read aligned to the byte-arrays already read.
      if buffered > 0 and buffered % alignment_ == 0:
        return buffer_.read_bytes buffer_.buffered

      // Read out to avoid going above max_size.
      if buffered >= max_size_:
        return buffer_.read_bytes max_size_

      // Force a read. If we cannot read all, we are at end but have bytes.
      if not buffer_.can_ensure max_size_:
        return buffer_.read_bytes buffer_.buffered
