#!/usr/bin/env bash

# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

set -euo pipefail

CURR_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
BUILD_INFO="${CURR_DIR}/../src/host/cli/build-info.toit"

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "Usage: $0 <version>" >&2
  exit 2
fi

BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "Updating ${BUILD_INFO}"
sed -i.bak \
  -e "s/^JAG-VERSION ::= .*/JAG-VERSION ::= \"${VERSION}\"/" \
  -e "s/^BUILD-DATE ::= .*/BUILD-DATE ::= \"${BUILD_DATE}\"/" \
  -e "s/^IS-RELEASE ::= .*/IS-RELEASE ::= true/" \
  "${BUILD_INFO}"
rm "${BUILD_INFO}.bak"
