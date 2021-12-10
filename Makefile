# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := build
CURR_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

GO_SOURCE := $(shell find . -name '*.go')
TOIT_SOURCE := $(shell find . -name '*.toit')
THIRD_PARTY_TOIT_PATH = $(CURR_DIR)/third_party/toit
TOIT_REPO_PATH ?= $(THIRD_PARTY_TOIT_PATH)
JAG_TOIT_PATH ?= $(TOIT_REPO_PATH)/build/host/sdk

$(BUILD_DIR):
	mkdir -p $@

.PHONY: jag
jag: $(BUILD_DIR)/jag

$(BUILD_DIR)/jag: $(GO_SOURCE) $(BUILD_DIR)
	CGO_ENABLED=1 GODEBUG=netdns=go go build  -o $@ ./cmd/jag

.PHONY: snapshot
snapshot: $(BUILD_DIR)/jaguar.snapshot

.PHONY: $(THIRD_PARTY_TOIT_PATH)/build/host/sdk/bin/toitc
$(THIRD_PARTY_TOIT_PATH)/build/host/sdk/bin/toitc:
	make -C $(THIRD_PARTY_TOIT_PATH) build/host/sdk/bin/toitc

$(BUILD_DIR)/jaguar.snapshot: $(JAG_TOIT_PATH)/bin/toitc $(TOIT_SOURCE) $(BUILD_DIR)
	$(JAG_TOIT_PATH)/bin/toitc -w ./$@ ./src/jaguar.toit

IDF_PATH ?= $(TOIT_REPO_PATH)/third_party/esp-idf
.PHONY: $(TOIT_REPO_PATH)/build/host/esp32/
$(TOIT_REPO_PATH)/build/host/esp32/: $(TOIT_SOURCE)
	IDF_PATH=$(IDF_PATH) make -C $(TOIT_REPO_PATH) esp32 ESP32_ENTRY=$(CURR_DIR)/src/jaguar.toit esp32

$(BUILD_DIR)/image.snapshot: $(TOIT_REPO_PATH)/build/host/esp32/
	cp $(TOIT_REPO_PATH)/build/snapshot $@

.PHONY: image_snapshot
image_snapshot: $(BUILD_DIR)/image.snapshot

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

.PHONY: install_esp_idf
install_esp_idf:
	IDF_PATH=$(IDF_PATH) $(IDF_PATH)/install.sh

clean:
	rm -rf $(BUILD_DIR)
