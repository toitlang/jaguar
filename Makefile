# Copyright (C) 2021 Toitware ApS. All rights reserved.
# Use of this source code is governed by an MIT-style license that can be
# found in the LICENSE file.

BUILD_DIR := $(CURDIR)/build

ifeq ($(OS),Windows_NT)
  EXE_SUFFIX := .exe
else
  EXE_SUFFIX :=
endif

ifdef JAG_TOIT_REPO_PATH
  TOIT ?= $(JAG_TOIT_REPO_PATH)/build/host/sdk/bin/toit$(EXE_SUFFIX)
else
  TOIT ?= toit$(EXE_SUFFIX)
endif

JAG_BINARY ?= $(BUILD_DIR)/jag$(EXE_SUFFIX)
JAG_HOST_ENTRY_POINT := $(CURDIR)/src/jag.toit
JAG_DEVICE_ENTRY_POINT := $(CURDIR)/src/jaguar.toit
JAG_TOIT_SOURCES := $(shell find src -name '*.toit')
JAG_PACKAGE_FILES := package.lock package.yaml
JAG_TEST_SOURCES := $(shell find tests -name '*.toit')

.PHONY: all
all: jag assets

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

.PHONY: install-dependencies
install-dependencies:
	$(TOIT) pkg --project-root=$(CURDIR) install

.PHONY: jag
jag: $(JAG_BINARY)

$(JAG_BINARY): $(JAG_TOIT_SOURCES) $(JAG_PACKAGE_FILES)
	mkdir -p $(dir $@)
	$(TOIT) compile -O2 $(TOIT_COMPILE_FLAGS) -o $@ $(JAG_HOST_ENTRY_POINT)

.PHONY: assets
assets: $(BUILD_DIR)/assets/jaguar.snapshot

$(BUILD_DIR)/assets/jaguar.snapshot: $(JAG_TOIT_SOURCES) $(JAG_PACKAGE_FILES)
	mkdir -p $(dir $@)
	$(TOIT) compile -Werror -O2 --snapshot -o $@ $(JAG_DEVICE_ENTRY_POINT)

.PHONY: analyze
analyze:
	$(TOIT) analyze -Werror $(JAG_TOIT_SOURCES) $(JAG_TEST_SOURCES)

.PHONY: unit-test
unit-test: analyze
	@for test_file in tests/*-test.toit; do \
		set -e; \
		echo "$$test_file"; \
		$(TOIT) run "$$test_file"; \
	done

.PHONY: completion-test
completion-test: jag
	$(JAG_BINARY) completion bash | grep -q '__complete'
	$(JAG_BINARY) completion zsh | grep -q 'compdef'
	$(JAG_BINARY) completion fish | grep -q 'complete -c'
	$(JAG_BINARY) completion powershell | grep -q 'Register-ArgumentCompleter'

.PHONY: integration-test
integration-test: all
	JAG_BINARY=$(JAG_BINARY) tests/integration/run-host-test.sh

.PHONY: test
test: unit-test completion-test integration-test

.PHONY: qemu-test-esp32
qemu-test-esp32: all
	JAG_BINARY=$(JAG_BINARY) tests/qemu/run-jaguar-test.sh esp32

.PHONY: qemu-test-esp32s3
qemu-test-esp32s3: all
	JAG_BINARY=$(JAG_BINARY) tests/qemu/run-jaguar-test.sh esp32s3

.PHONY: qemu-test
qemu-test: qemu-test-esp32 qemu-test-esp32s3
