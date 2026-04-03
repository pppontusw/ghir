BIN ?= ticket-runner
PKG ?= .
INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_NAME ?= ghir

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.5.1

.PHONY: help build install run fmt lint typecheck q test t worktree-setup worktree-up worktree-show worktree-clean

help:
	@echo "Targets:"
	@echo "  make build                Build local binary ./$(BIN)"
	@echo "  make install              Install binary to $(INSTALL_DIR)/$(INSTALL_NAME)"
	@echo "  make run ARGS=\"...\"       Run via go run with optional ARGS"
	@echo "  make q                    Run format, lint, typecheck, build"
	@echo "  make t                    Run tests"
	@echo "  make worktree-setup       Download dependencies"
	@echo "  make worktree-up          No-op (CLI project)"
	@echo "  make worktree-show        Show worktree status"
	@echo "  make worktree-clean       Remove the current worktree"

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

install:
	mkdir -p $(INSTALL_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(INSTALL_DIR)/$(INSTALL_NAME) $(PKG)
	@echo "Installed: $(INSTALL_DIR)/$(INSTALL_NAME)"

run:
	go run -ldflags "$(LDFLAGS)" $(PKG) $(ARGS)

fmt:
	gofmt -w .

lint:
	go vet ./...
	$(STATICCHECK) -checks U1000 ./...

typecheck:
	go test ./... -run '^$$'

q: fmt lint typecheck build

test:
	go test ./...

t: test

worktree-setup:
	go mod download

worktree-up:
	@echo "ghir is a CLI project; no long-running services to start"

worktree-show:
	@echo "worktree=$$(basename \"$$(pwd)\")"
	@echo "binary=$(BIN)"
	@echo "service=not_applicable"

worktree-clean:
	@set -e; \
	if [ ! -f .git ]; then \
		echo "worktree-clean must run from a git worktree (not from the main clone)."; \
		exit 1; \
	fi; \
	rm -f $(BIN); \
	wt_name=$$(basename "$$(pwd)"); \
	worktree_path="$$(pwd)"; \
	git_common_dir=$$(git rev-parse --git-common-dir); \
	main_repo_path=$$(dirname "$$git_common_dir"); \
	cd "$$main_repo_path" && git worktree remove "$$worktree_path"; \
	echo "Removed worktree $$wt_name"
