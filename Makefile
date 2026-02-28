BIN ?= ticket-runner
PKG ?= .
INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_NAME ?= ghir

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help build install run

help:
	@echo "Targets:"
	@echo "  make build                Build local binary ./$(BIN)"
	@echo "  make install              Install binary to $(INSTALL_DIR)/$(INSTALL_NAME)"
	@echo "  make run ARGS=\"...\"       Run via go run with optional ARGS"

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

install:
	mkdir -p $(INSTALL_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(INSTALL_DIR)/$(INSTALL_NAME) $(PKG)
	@echo "Installed: $(INSTALL_DIR)/$(INSTALL_NAME)"

run:
	go run -ldflags "$(LDFLAGS)" $(PKG) $(ARGS)
