# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := build
CURR_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

GO_SOURCE := $(shell find . -name '*.go')
TOIT_SOURCE := $(shell find . -name '*.toit') package.lock package.yaml
THIRD_PARTY_TOIT_PATH = $(CURR_DIR)/third_party/toit
TOIT_REPO_PATH ?= $(THIRD_PARTY_TOIT_PATH)
JAG_TOIT_PATH ?= $(TOIT_REPO_PATH)/build/host/sdk

BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
BUILD_VERSION ?= $(shell ./tools/gitversion)
BUILD_SDK_VERSION = $(shell cd ./third_party/toit; ./../../tools/gitversion)

JAG_BINARY ?= jag

.PHONY: jag
jag: $(BUILD_DIR)/$(JAG_BINARY)

$(BUILD_DIR):
	mkdir -p $@

.PHONY: update-jag-info
update-jag-info: $(BUILD_DIR)
	sed 's/date       = .*/date       = "$(BUILD_DATE)"/' $(CURR_DIR)/cmd/jag/main.go | \
	sed 's/version    = .*/version    = "$(BUILD_VERSION)"/' | \
	sed 's/sdkVersion = .*/sdkVersion = "$(BUILD_SDK_VERSION)"/' > $(BUILD_DIR)/new_main.go
	mv $(BUILD_DIR)/new_main.go $(CURR_DIR)/cmd/jag/main.go

GO_BUILD_FLAGS := CGO_ENABLED=1 GODEBUG=netdns=go
GO_LINK_FLAGS := $(GO_LINK_FLAGS) -extldflags '-static'

$(BUILD_DIR)/$(JAG_BINARY): $(GO_SOURCE) $(BUILD_DIR)
	$(GO_BUILD_FLAGS) go build -tags 'netgo osusergo' -ldflags "$(GO_LINK_FLAGS)" -o $@ ./cmd/jag

.PHONY: snapshot
snapshot: $(BUILD_DIR)/jaguar.snapshot

.PHONY: $(TOIT_REPO_PATH)/build/host/sdk/bin/toitpkg
$(TOIT_REPO_PATH)/build/host/sdk/bin/toitpkg:
	make -C $(TOIT_REPO_PATH) build/host/sdk/bin/toitpkg

.packages: $(TOIT_SOURCE) $(JAG_TOIT_PATH)/bin/toitpkg
	$(JAG_TOIT_PATH)/bin/toitpkg pkg install

.PHONY: $(TOIT_REPO_PATH)/build/host/sdk/bin/toitc
$(TOIT_REPO_PATH)/build/host/sdk/bin/toitc:
	make -C $(TOIT_REPO_PATH) build/host/sdk/bin/toitc

$(BUILD_DIR)/jaguar.snapshot: $(JAG_TOIT_PATH)/bin/toitc $(TOIT_SOURCE) $(BUILD_DIR) .packages
	$(JAG_TOIT_PATH)/bin/toitc -w ./$@ ./src/jaguar.toit

IDF_PATH ?= $(TOIT_REPO_PATH)/third_party/esp-idf
.PHONY: $(TOIT_REPO_PATH)/build/host/esp32/
$(TOIT_REPO_PATH)/build/host/esp32/: $(TOIT_SOURCE) .packages
	IDF_PATH=$(IDF_PATH) make -C $(TOIT_REPO_PATH) esp32 ESP32_ENTRY=$(CURR_DIR)/src/jaguar.toit esp32

$(BUILD_DIR)/image.snapshot: $(TOIT_REPO_PATH)/build/host/esp32/ .packages
	cp $(TOIT_REPO_PATH)/build/snapshot $@

.PHONY: image-snapshot
image-snapshot: $(BUILD_DIR)/image.snapshot

$(BUILD_DIR)/image/:
	mkdir -p $@

$(BUILD_DIR)/image/ota_data_initial.bin: $(TOIT_REPO_PATH)/build/host/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_REPO_PATH)/build/esp32/ota_data_initial.bin $@

$(BUILD_DIR)/image/bootloader/:
	mkdir -p $@

$(BUILD_DIR)/image/bootloader/bootloader.bin: $(TOIT_REPO_PATH)/build/host/esp32/ $(BUILD_DIR)/image/bootloader/
	cp $(TOIT_REPO_PATH)/build/esp32/bootloader/bootloader.bin $@

$(BUILD_DIR)/image/toit.bin: $(TOIT_REPO_PATH)/build/host/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_REPO_PATH)/build/esp32/toit.bin $@

$(BUILD_DIR)/image/partitions.bin: $(TOIT_REPO_PATH)/build/host/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_REPO_PATH)/build/esp32/partitions.bin $@

.PHONY: image
image: $(BUILD_DIR)/image.snapshot $(BUILD_DIR)/image/ota_data_initial.bin $(BUILD_DIR)/image/bootloader/bootloader.bin $(BUILD_DIR)/image/toit.bin $(BUILD_DIR)/image/partitions.bin

.PHONY: install-esp-idf
install-esp-idf:
	IDF_PATH=$(IDF_PATH) $(IDF_PATH)/install.sh

clean:
	rm -rf $(BUILD_DIR)
