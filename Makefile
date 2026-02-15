BIN ?= ticket-runner
PKG ?= .
INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_NAME ?= ghir

.PHONY: help build install run

help:
	@echo "Targets:"
	@echo "  make build                Build local binary ./$(BIN)"
	@echo "  make install              Install binary to $(INSTALL_DIR)/$(INSTALL_NAME)"
	@echo "  make run ARGS=\"...\"       Run via go run with optional ARGS"

build:
	go build -o $(BIN) $(PKG)

install:
	mkdir -p $(INSTALL_DIR)
	go build -o $(INSTALL_DIR)/$(INSTALL_NAME) $(PKG)
	@echo "Installed: $(INSTALL_DIR)/$(INSTALL_NAME)"

run:
	go run $(PKG) $(ARGS)
