# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := $(CURDIR)/build
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

# Setup Go compilation flags.
GO_BUILD_FLAGS :=
GO_LINK_FLAGS += -X 'main.buildDate="$(BUILD_DATE)"'
ifdef JAG_BUILD_RELEASE
GO_LINK_FLAGS += -X 'main.buildMode=release'
endif

.PHONY: all
all: jag assets

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

#############################
# Rules for the Jaguar binary
#############################
.PHONY: jag
jag: $(BUILD_DIR)/$(JAG_BINARY)

$(BUILD_DIR)/$(JAG_BINARY): $(JAG_GO_SOURCES)
	$(GO_BUILD_FLAGS) go build -ldflags "$(GO_LINK_FLAGS)" -o $@ ./cmd/jag

#############################
# Rules for the Jaguar assets
#############################
.PHONY: assets
assets: $(BUILD_DIR)/assets/jaguar.snapshot

$(BUILD_DIR)/assets/jaguar.snapshot: install-dependencies
$(BUILD_DIR)/assets/jaguar.snapshot: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
$(BUILD_DIR)/assets/jaguar.snapshot: $(JAG_TOIT_SOURCES)
	mkdir -p $(BUILD_DIR)/assets
	$(SDK_PATH)/bin/toit$(EXE_SUFFIX) compile -O2 --snapshot -o $@ $(JAG_ENTRY_POINT)

.PHONY: install-dependencies
install-dependencies: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
	$(SDK_PATH)/bin/toit$(EXE_SUFFIX) pkg --project-root=$(CURDIR) install

############################################
# Rules to build with JAG_TOIT_REPO_PATH set
############################################

ifdef JAG_TOIT_REPO_PATH
all: $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

JAG_TOIT_DEPENDENCIES  = $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
JAG_TOIT_DEPENDENCIES += $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

# We use a marker in the build directory to avoid
# recompiling the SDK multiple times during one
# invocation of this Makefile.
SDK_BUILD_MARKER := $(BUILD_DIR)/sdk.build
$(JAG_TOIT_DEPENDENCIES): force-rebuild-sdk
$(JAG_TOIT_DEPENDENCIES): $(SDK_BUILD_MARKER)

# The SDK build marker is *not* phony, so we only
# use the rule once per invocation of this Makefile.
$(SDK_BUILD_MARKER):
	make -C $(JAG_TOIT_REPO_PATH) version-file esp32
	mkdir -p $(BUILD_DIR)
	echo "$(BUILD_DATE)" > $@

.PHONY: all-chips
all-chips:
	make -C $(JAG_TOIT_REPO_PATH) ESP32_CHIP=esp32c3 esp32
	make -C $(JAG_TOIT_REPO_PATH) ESP32_CHIP=esp32c6 esp32
	make -C $(JAG_TOIT_REPO_PATH) ESP32_CHIP=esp32s2 esp32
	make -C $(JAG_TOIT_REPO_PATH) ESP32_CHIP=esp32s3 esp32

.PHONY: force-rebuild-sdk
force-rebuild-sdk:
	rm -f $(SDK_BUILD_MARKER)
endif

###############################################
# Rules to build without JAG_TOIT_REPO_PATH set
###############################################

.PHONY: download-sdk
download-sdk: $(BUILD_DIR)/$(JAG_BINARY)
	rm -rf $(BUILD_SDK_DIR)
	$(BUILD_DIR)/$(JAG_BINARY) --no-analytics setup sdk $(BUILD_SDK_DIR)

.PHONY: test
test: $(BUILD_DIR)/$(JAG_BINARY)
	@# For now just try to extract images for all chips.
	@for chip in esp32 esp32c3 esp32c6 esp32s2 esp32s3; do \
		set -e; \
		tmp_dir=$$(mktemp -d); \
		$(BUILD_DIR)/$(JAG_BINARY) \
				--no-analytics \
				--wifi-ssid=test --wifi-password=test \
				firmware extract $$chip \
				-o $$tmp_dir/$$chip.snapshot; \
		rm -rf $$tmp_dir; \
	done
