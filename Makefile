# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := $(CURDIR)/build
BUILD_SDK_DIR := $(BUILD_DIR)/sdk
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

ifeq ($(OS),Windows_NT)
	EXE_SUFFIX := .exe
	DETECTED_OS := Windows
else
	EXE_SUFFIX :=
	DETECTED_OS := $(shell uname)
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

# Go build flags
GO_BUILD_FLAGS :=
GO_LINK_FLAGS := -X 'main.buildDate=$(BUILD_DATE)'
ifdef JAG_BUILD_RELEASE
	GO_LINK_FLAGS += -X 'main.buildMode=release'
endif

# When developing with local Toit repo, ALWAYS use its host SDK compiler
# This overrides any cached SDK for consistency during make assets / make test
ifdef JAG_TOIT_REPO_PATH
  TOIT_COMPILER := $(JAG_TOIT_REPO_PATH)/build/host/sdk/bin/toit$(EXE_SUFFIX)
  # Force package install and snapshot compilation to use local compiler
  override SDK_PATH := $(JAG_TOIT_REPO_PATH)/build/host/sdk
endif


.PHONY: all clean

all: jag assets sync-cache

clean:
	rm -rf $(BUILD_DIR)

#############################
# Jaguar Go binary
#############################
.PHONY: jag
jag: assets sync-cache
jag: $(BUILD_DIR)/$(JAG_BINARY)

$(BUILD_DIR)/$(JAG_BINARY): $(JAG_GO_SOURCES)
	go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LINK_FLAGS)" -o $@ ./cmd/jag

#############################
# Jaguar assets (snapshot)
#############################
.PHONY: assets
assets: $(BUILD_DIR)/assets/jaguar.snapshot

$(BUILD_DIR)/assets/jaguar.snapshot: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
$(BUILD_DIR)/assets/jaguar.snapshot: $(JAG_TOIT_SOURCES)

.PHONY: install-dependencies
install-dependencies: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
	@echo "Installing Toit packages (using $(SDK_PATH)/bin/toit$(EXE_SUFFIX))..."
	$(SDK_PATH)/bin/toit$(EXE_SUFFIX) pkg --project-root=$(CURDIR) install

$(BUILD_DIR)/assets/jaguar.snapshot: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
$(BUILD_DIR)/assets/jaguar.snapshot: $(JAG_TOIT_SOURCES)
$(BUILD_DIR)/assets/jaguar.snapshot: install-dependencies
	mkdir -p $(dir $@)
	@echo "Building jaguar.snapshot (using $(SDK_PATH)/bin/toit$(EXE_SUFFIX))..."
	$(SDK_PATH)/bin/toit$(EXE_SUFFIX) compile -O2 --snapshot -o $@ $(JAG_ENTRY_POINT)


############################################
# When using external Toit repo (JAG_TOIT_REPO_PATH)
############################################
ifdef JAG_TOIT_REPO_PATH

# Ensure firmware for all chips is built
all: $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

JAG_TOIT_DEPENDENCIES := $(SDK_PATH)/bin/toit$(EXE_SUFFIX)
JAG_TOIT_DEPENDENCIES += $(JAG_TOIT_REPO_PATH)/build/esp32/firmware.envelope

SDK_BUILD_MARKER := $(BUILD_DIR)/sdk.build

$(JAG_TOIT_DEPENDENCIES): $(SDK_BUILD_MARKER)

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

############################################
# When NO JAG_TOIT_REPO_PATH → download SDK
############################################
ifndef JAG_TOIT_REPO_PATH

.PHONY: download-sdk
download-sdk: $(BUILD_DIR)/$(JAG_BINARY)
	rm -rf $(BUILD_SDK_DIR)
	$(BUILD_DIR)/$(JAG_BINARY) --no-analytics setup sdk $(BUILD_SDK_DIR)

# Ensure SDK is downloaded before building snapshot or testing
$(SDK_PATH)/bin/toit$(EXE_SUFFIX): download-sdk

endif


############################################
# Install Jaguar binary to /usr/local/bin
############################################
PREFIX ?= /usr/local
BINDIR := $(PREFIX)/bin

.PHONY: install
install: jag
	@echo "Installing jag binary to $(BINDIR)/jag ..."
	@install -d $(BINDIR)
	@install -m 755 $(BUILD_DIR)/$(JAG_BINARY) $(BINDIR)/jag
	@echo "jag successfully installed to $(BINDIR)"

.PHONY: uninstall
uninstall:
	@echo "Uninstalling jag from $(BINDIR)"
	@rm -f $(BINDIR)/jag
	@echo "jag binary removed from $(BINDIR)"

############################################
# Test rule – ensures assets are built first
############################################
.PHONY: test
test: jag       
test: assets                                   # Critical: forces snapshot rebuild if needed
test: sync-cache
test: $(SDK_PATH)/bin/toit$(EXE_SUFFIX)        # Ensures SDK is ready
	@echo "Running firmware extract tests for chips with available firmware envelopes..."
	@success_count=0; \
	skip_count=0; \
	failed=0; \
	for chip in esp32 esp32c3 esp32s2 esp32s3; do \
	    envelope_path=""; \
	    if [ -n "$(JAG_TOIT_REPO_PATH)" ]; then \
	        envelope_path="$(JAG_TOIT_REPO_PATH)/build/$$chip/firmware.envelope"; \
	    else \
	        envelope_path="$(SDK_PATH)/../$$chip/firmware.envelope"; \
	    fi; \
	    if [ -f "$$envelope_path" ]; then \
	        echo "Testing $$chip..."; \
	        tmp_dir=$$(mktemp -d); \
	        if $(BUILD_DIR)/$(JAG_BINARY) \
	            --no-analytics \
	            --wifi-ssid=test --wifi-password=test \
	            firmware extract $$chip \
	            -o "$$tmp_dir/$$chip.snapshot"; \
	        then \
	            echo "$$chip: OK"; \
	            success_count=$$((success_count + 1)); \
	        else \
	            echo "$$chip: FAILED"; \
	            failed=$$((failed + 1)); \
	        fi; \
	        rm -rf "$$tmp_dir"; \
	    else \
	        echo "$$chip: SKIPPED (no firmware envelope at $$envelope_path)"; \
	        skip_count=$$((skip_count + 1)); \
	    fi; \
	done; \
	echo ""; \
	echo "Results:"; \
	[ $$success_count -gt 0 ] && echo "  Success: $$success_count chip(s)"; \
	[ $$skip_count -gt 0 ] && echo "  Skipped: $$skip_count chip(s)"; \
	[ $$failed -gt 0 ] && echo "  FAILED:  $$failed chip(s)"; \
	if [ $$failed -gt 0 ]; then \
	    echo "Test failed."; \
	    exit 1; \
	elif [ $$success_count -eq 0 ]; then \
	    echo "Warning: No chips were tested."; \
	    exit 1; \
	else \
	    echo "All available tests passed."; \
	    exit 0; \
	fi  

############################################
# Sync local snapshot to user cache
############################################

CACHE_ASSETS_DIR := $(HOME)/.cache/jaguar/assets
CACHE_SNAPSHOT := $(CACHE_ASSETS_DIR)/jaguar.snapshot

.PHONY: sync-cache
sync-cache: assets
	@echo "Syncing local snapshot to user cache..."
	@mkdir -p $(CACHE_ASSETS_DIR)
	@if [ ! -f "$(CACHE_SNAPSHOT)" ]; then \
	    echo "Cache snapshot missing — copying from build"; \
	    cp $(BUILD_DIR)/assets/jaguar.snapshot $(CACHE_SNAPSHOT); \
	elif ! cmp -s $(BUILD_DIR)/assets/jaguar.snapshot $(CACHE_SNAPSHOT); then \
	    echo "Cache snapshot outdated — updating"; \
	    cp $(BUILD_DIR)/assets/jaguar.snapshot $(CACHE_SNAPSHOT); \
	else \
	    echo "Cache snapshot already up-to-date"; \
	fi	
# Optional: make 'all' also sync to cache (useful for development)
all: sync-cache
