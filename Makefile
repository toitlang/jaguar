BUILD_DIR := build

GO_SOURCE := $(shell find . -name '*.go')

.PHONY: jag
jag: $(BUILD_DIR)/jag

$(BUILD_DIR)/jag: $(GO_SOURCE)
	CGO_ENABLED=1 GODEBUG=netdns=go go build  -o $@ ./cmd/jag

clean:
	rm -rf $(BUILD_DIR)
