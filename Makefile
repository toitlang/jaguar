BUILD_DIR := build

GO_SOURCE := $(shell find . -name '*.go')

.PHONY: shag
shag: $(BUILD_DIR)/shag

$(BUILD_DIR)/shag: $(GO_SOURCE)
	CGO_ENABLED=1 GODEBUG=netdns=go go build  -o $@ ./cmd/shaguar

clean:
	rm -rf $(BUILD_DIR)
