# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := build

GO_SOURCE := $(shell find . -name '*.go')
TOIT_SOURCE := $(shell find . -name '*.toit')

.PHONY: jag
jag: $(BUILD_DIR)/jag

$(BUILD_DIR)/jag: $(GO_SOURCE)
	CGO_ENABLED=1 GODEBUG=netdns=go go build  -o $@ ./cmd/jag

.PHONY: snapshot
snapshot: $(BUILD_DIR)/jaguar.snapshot

$(BUILD_DIR)/jaguar.snapshot: check-toitc-env $(TOIT_SOURCE)
	$(JAGUAR_TOITC_PATH) -w $@ ./src/jaguar.toit

clean:
	rm -rf $(BUILD_DIR)


check-toitc-env:
ifndef JAGUAR_TOITC_PATH
	$(error JAGUAR_TOITC_PATH is not set)
endif
