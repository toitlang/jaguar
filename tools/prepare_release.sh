#!/usr/bin/env bash
set -e

CURR_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

VERSION=$1
echo "Updating cmd/jag/main.go"
if [[ "$VERSION" == "" ]]; then
    make -C $CURR_DIR/../ update_jag_info
else
    BUILD_VERSION=$VERSION make -C $CURR_DIR/../ update_jag_info
fi
