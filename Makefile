# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := build
BUILD_SDK_DIR := $(CURDIR)/build/sdk
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

ifeq ($(OS),Windows_NT)
  EXE_SUFFIX=.exe
  DETECTED_OS=$(OS)
else
  EXE_SUFFIX=
  DETECTED_OS=$(shell uname)
endif


ifdef JAG_TOIT_REPO_PATH
SDK_PATH := $(JAG_TOIT_REPO_PATH)/build/host/sdk
else
SDK_PATH := $(BUILD_SDK_DIR)
endif

JAG_BINARY := jag$(EXE_SUFFIX)
JAG_ENTRY_POINT := $(CURDIR)/src/jaguar.toit
JAG_TOIT_SOURCES := $(shell find src -name '*.toit') package.lock package.yaml
JAG_GO_SOURCES := $(shell find cmd -name '*.go')

.PHONY: all
all: jag assets

.PHONY: jag
jag: $(BUILD_DIR)/$(JAG_BINARY)

$(BUILD_DIR):
	mkdir -p $@

.PHONY: update-jag-info
update-jag-info: $(BUILD_DIR)
	sed 's/date       = .*/date       = "$(BUILD_DATE)"/' $(CURDIR)/cmd/jag/main.go > $(BUILD_DIR)/new_main.go
	mv $(BUILD_DIR)/new_main.go $(CURDIR)/cmd/jag/main.go

GO_BUILD_FLAGS := CGO_ENABLED=1 GODEBUG=netdns=go
GO_LINK_FLAGS := $(GO_LINK_FLAGS) -extldflags '-static'
ifdef JAG_BUILD_RELEASE
GO_LINK_FLAGS += -X 'main.buildMode=release'
endif

$(BUILD_DIR)/$(JAG_BINARY): $(JAG_GO_SOURCES) $(BUILD_DIR)
	$(GO_BUILD_FLAGS) go build -tags 'netgo osusergo' -ldflags "$(GO_LINK_FLAGS)" -o $@ ./cmd/jag

.PHONY: jag-macos-sign
jag-macos-sign:
	gon -log-level=debug -log-json ./tools/gon.json

.PHONY: assets
assets: $(BUILD_DIR)/assets/jaguar.snapshot

$(BUILD_DIR)/assets/jaguar.snapshot: install-dependencies
$(BUILD_DIR)/assets/jaguar.snapshot: $(JAG_TOIT_SOURCES)
$(BUILD_DIR)/assets/jaguar.snapshot: $(SDK_PATH)/bin/toit.compile $(BUILD_DIR)/assets/
	$(SDK_PATH)/bin/toit.compile -w $@ $(JAG_ENTRY_POINT)

$(BUILD_DIR)/assets/:
	mkdir -p $@

.PHONY: install-dependencies
install-dependencies: $(SDK_PATH)/bin/toit.pkg
	$(SDK_PATH)/bin/toit.pkg --project-root=$(CURDIR) install

clean:
	rm -rf $(BUILD_DIR)

###############################################
# Rules to build without JAG_TOIT_REPO_PATH set
###############################################

.PHONY: download-sdk
download-sdk: $(BUILD_DIR)/$(JAG_BINARY)
	rm -rf $(BUILD_SDK_DIR)
	$(BUILD_DIR)/$(JAG_BINARY) --no-analytics setup sdk $(BUILD_SDK_DIR)

############################################
# Rules to build with JAG_TOIT_REPO_PATH set
############################################

ifdef JAG_TOIT_REPO_PATH
all: $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

JAG_TOIT_DEPENDENCIES  = $(SDK_PATH)/bin/toit.compile
JAG_TOIT_DEPENDENCIES += $(SDK_PATH)/bin/toit.pkg
JAG_TOIT_DEPENDENCIES += $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

.PHONY: $(JAG_TOIT_DEPENDENCIES)
$(JAG_TOIT_DEPENDENCIES): toit-repo-git-tags
	make -C $(JAG_TOIT_REPO_PATH) version-file esp32

.PHONY: toit-repo-git-tags
toit-repo-git-tags:
	(cd $(JAG_TOIT_REPO_PATH); git fetch --tags --recurse-submodules=no)
endif
