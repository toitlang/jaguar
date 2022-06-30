# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := build

ifeq ($(OS),Windows_NT)
  EXE_SUFFIX=.exe
  DETECTED_OS=$(OS)
else
  EXE_SUFFIX=
  DETECTED_OS=$(shell uname)
endif

GO_SOURCE := $(shell find cmd -name '*.go')
TOIT_SOURCE := $(shell find src -name '*.toit') package.lock package.yaml
JAG_TOIT_REPO_PATH ?= $(CURDIR)/third_party/toit

TOIT_PATH := $(JAG_TOIT_REPO_PATH)
JAG_TOIT_PATH := $(TOIT_PATH)/build/host/sdk
JAG_ENTRY_POINT := $(CURDIR)/src/jaguar.toit
IDF_PATH := $(TOIT_PATH)/third_party/esp-idf

# The following variables are only executed when used.
# This way we don't run the commands when the Makefile is run for
#   commands that don't use them.
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
BUILD_VERSION = $(shell ./tools/gitversion)
BUILD_SDK_VERSION = $(shell cd ./third_party/toit; ./../../tools/gitversion)

JAG_BINARY := jag$(EXE_SUFFIX)

# The default ("all") target is deliberately not used on the
# continuous builders, so we do not need to worry about failing
# the setup check there.
.PHONY: all
all: jag image
	$(BUILD_DIR)/$(JAG_BINARY) setup --check

.PHONY: jag
jag: $(BUILD_DIR)/$(JAG_BINARY)

$(BUILD_DIR):
	mkdir -p $@

.PHONY: update-jag-info
update-jag-info: $(BUILD_DIR)
	sed 's/date       = .*/date       = "$(BUILD_DATE)"/' $(CURDIR)/cmd/jag/main.go | \
	sed 's/version    = .*/version    = "$(BUILD_VERSION)"/' | \
	sed 's/sdkVersion = .*/sdkVersion = "$(BUILD_SDK_VERSION)"/' > $(BUILD_DIR)/new_main.go
	mv $(BUILD_DIR)/new_main.go $(CURDIR)/cmd/jag/main.go

GO_BUILD_FLAGS := CGO_ENABLED=1 GODEBUG=netdns=go
GO_LINK_FLAGS := $(GO_LINK_FLAGS) -extldflags '-static'
ifdef JAG_BUILD_RELEASE
GO_LINK_FLAGS += -X 'main.buildMode=release'
endif

$(BUILD_DIR)/$(JAG_BINARY): $(GO_SOURCE) $(BUILD_DIR)
	$(GO_BUILD_FLAGS) go build -tags 'netgo osusergo' -ldflags "$(GO_LINK_FLAGS)" -o $@ ./cmd/jag

$(BUILD_DIR)/macos:
	mkdir -p $@

.PHONY: jag-macos-sign
jag-macos-sign:
	gon -log-level=debug -log-json ./tools/gon.json

.PHONY: toit-git-tags
toit-git-tags:
	(cd $(TOIT_PATH); git fetch --tags --recurse-submodules=no)

.PHONY: $(JAG_TOIT_PATH)/bin/toit.compiler $(JAG_TOIT_PATH)/bin/toit.pkg
$(JAG_TOIT_PATH)/bin/toit.compiler $(JAG_TOIT_PATH)/bin/toit.pkg: toit-git-tags
	make -C $(TOIT_PATH) sdk

.packages: $(JAG_TOIT_PATH)/bin/toit.pkg $(TOIT_SOURCE)
	$(JAG_TOIT_PATH)/bin/toit.pkg install

.PHONY: $(TOIT_PATH)/build/esp32/
$(TOIT_PATH)/build/esp32/: $(TOIT_SOURCE) .packages toit-git-tags install-dependencies
	make -C $(TOIT_PATH) ESP32_ENTRY=$(JAG_ENTRY_POINT) esp32

$(BUILD_DIR)/image/:
	mkdir -p $@

$(BUILD_DIR)/image/bootloader/:
	mkdir -p $@

$(BUILD_DIR)/image/toit.bin: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_PATH)/build/esp32/toit.bin $@

$(BUILD_DIR)/image/toit.elf: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_PATH)/build/esp32/toit.elf $@

$(BUILD_DIR)/image/bootloader/bootloader.bin: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/bootloader/
	cp $(TOIT_PATH)/build/esp32/bootloader/bootloader.bin $@

$(BUILD_DIR)/image/partitions.bin: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_PATH)/build/esp32/partitions.bin $@

$(BUILD_DIR)/image/system.snapshot: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_PATH)/build/esp32/system.snapshot $@

$(BUILD_DIR)/image/jaguar.snapshot: $(TOIT_PATH)/build/esp32/ $(BUILD_DIR)/image/
	cp $(TOIT_PATH)/build/esp32/program.snapshot $@

.PHONY: image
image: $(BUILD_DIR)/image/toit.elf
image: $(BUILD_DIR)/image/toit.bin
image: $(BUILD_DIR)/image/bootloader/bootloader.bin
image: $(BUILD_DIR)/image/partitions.bin
image: $(BUILD_DIR)/image/system.snapshot
image: $(BUILD_DIR)/image/jaguar.snapshot

.PHONY: install-esp-idf
install-esp-idf:
	IDF_PATH=$(IDF_PATH) $(IDF_PATH)/install.sh

.PHONY: install-dependencies
install-dependencies: $(JAG_TOIT_PATH)/bin/toit.pkg
	$(JAG_TOIT_PATH)/bin/toit.pkg --auto-sync=false --project-root=$(CURDIR) install

clean:
	rm -rf $(BUILD_DIR)
