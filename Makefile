.PHONY: all build build-all build-origin build-node clean run test

BINARY_NAME=bete-node
BUILD_DIR=bin
export GOTMPDIR=/home/container/.gotmp

all: build

build:
	@echo "🔨 Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/app

build-origin:
	@echo "🔨 Building origin binary..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w" -o $(BUILD_DIR)/origin ./cmd/origin

build-node:
	@echo "🔨 Building node binary..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w" -o $(BUILD_DIR)/node ./cmd/node

build-all: build build-origin build-node
	@echo "🔨 Cross-compiling for Linux amd64 & arm64..."
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/app
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/app
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/app
	@echo "✅ All binaries built in $(BUILD_DIR)/"

clean:
	@rm -rf $(BUILD_DIR)
	@echo "🧹 Cleaned."

run:
	go run ./cmd/app

test:
	go test -v ./...
