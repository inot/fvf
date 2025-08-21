.PHONY: help build clean build-all macos-arm64 linux-amd64 linux-arm64 windows-amd64

# Project settings
APP_NAME ?= fvf
PKG := ./

# Version metadata (override via env if desired)
# Prefer ./version file if present; otherwise use git describe; finally default.
VERSION_FILE ?= version
ifeq (,$(VERSION))
  ifneq (,$(wildcard $(VERSION_FILE)))
    VERSION := $(shell sed -e 's/[[:space:]]//g' -e 's/[.]*$$//' $(VERSION_FILE))
  else
    VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
  endif
endif
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS := -s -w -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)' -X 'main.date=$(DATE)'
GOFLAGS ?=
CGO_ENABLED ?= 0

DIST := dist

help:
	@echo "Targets:"
	@echo "  make build            - Build for host platform"
	@echo "  make build-all        - Build for macOS arm64, Linux amd64/arm64, Windows amd64"
	@echo "  make macos-arm64      - Build darwin/arm64"
	@echo "  make linux-amd64      - Build linux/amd64"
	@echo "  make linux-arm64      - Build linux/arm64"
	@echo "  make windows-amd64    - Build windows/amd64"
	@echo "  make clean            - Remove dist directory"

$(DIST):
	@mkdir -p $(DIST)

build: $(DIST)
	@echo "Building host binary..."
	@CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -buildmode=pie -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME) $(PKG)
	@echo "Built $(DIST)/$(APP_NAME)"

macos-arm64: $(DIST)
	@echo "Building macOS arm64..."
	@GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -buildmode=pie -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-darwin-arm64 $(PKG)

linux-amd64: $(DIST)
	@echo "Building Linux amd64..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -buildmode=pie -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-linux-amd64 $(PKG)

linux-arm64: $(DIST)
	@echo "Building Linux arm64..."
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -buildmode=pie -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-linux-arm64 $(PKG)

windows-amd64: $(DIST)
	@echo "Building Windows amd64..."
	@GOOS=windows GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -buildmode=pie -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-windows-amd64.exe $(PKG)

build-all: macos-arm64 linux-amd64 linux-arm64 windows-amd64
	@echo "All cross-compiled binaries are in $(DIST)/"

clean:
	@rm -rf $(DIST)
	@echo "Cleaned $(DIST)/"
