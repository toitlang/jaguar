#!/usr/bin/env bash

# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -e

CURR_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

VERSION=$1
echo "Updating cmd/jag/main.go"
if [[ "$VERSION" == "" ]]; then
    make -C $CURR_DIR/../ update-jag-info
else
    BUILD_VERSION=$VERSION make -C $CURR_DIR/../ update-jag-info
fi
