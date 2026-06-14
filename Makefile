BINARY  := bin/aws_explorer
GO      := go
PKG     := ./...

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/ryandam9/aws_explorer/cmd.version=$(VERSION) \
           -X github.com/ryandam9/aws_explorer/cmd.commit=$(COMMIT) \
           -X github.com/ryandam9/aws_explorer/cmd.date=$(DATE)

.PHONY: all fmt vet test build install clean run run-all-regions tidy lint man docs help

all: fmt vet test build install

fmt:
	$(GO) fmt $(PKG)

vet:
	$(GO) vet $(PKG)

test:
	$(GO) test $(PKG) -v -count=1

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

# install copies the binary to a bin directory (override with PREFIX=...):
#   1) ~/.local/bin, if it is on $PATH
#   2) /usr/local/bin, if it exists
install: build
	@dest="$(PREFIX)"; \
	if [ -z "$$dest" ]; then \
		case ":$$PATH:" in *":$$HOME/.local/bin:"*) dest="$$HOME/.local/bin" ;; esac; \
	fi; \
	if [ -z "$$dest" ] && [ -d /usr/local/bin ]; then dest="/usr/local/bin"; fi; \
	if [ -z "$$dest" ]; then \
		echo "install: no suitable bin dir found; rerun with PREFIX=/path/to/bin"; exit 1; \
	fi; \
	mkdir -p "$$dest" && install -m 0755 $(BINARY) "$$dest/$(notdir $(BINARY))" && \
		echo "installed -> $$dest/$(notdir $(BINARY))"

clean:
	rm -f $(BINARY)
	rm -rf man docs/site

man: build
	./$(BINARY) docs --format man --dir man

docs: build
	./$(BINARY) docs --format markdown --dir docs/site
	./$(BINARY) docs --format html --dir docs/site
	@echo "Open docs/site/index.html in a browser, or browse docs/site/README.md"

run: build
	./$(BINARY)

run-all-regions: build
	./$(BINARY) --all-regions

tidy:
	$(GO) mod tidy

lint:
ifeq (, $(shell which golangci-lint))
	@echo "golangci-lint not installed, skipping"
else
	golangci-lint run
endif

help:
	@echo "Targets:"
	@echo "  fmt              - Format source code"
	@echo "  vet              - Run go vet"
	@echo "  test             - Run tests"
	@echo "  build            - Build binary"
	@echo "  install          - Build and install binary to a bin dir (PREFIX= to override)"
	@echo "  clean            - Remove binary and generated docs"
	@echo "  man              - Generate man pages into ./man"
	@echo "  docs             - Generate Markdown + HTML docs into ./docs/site"
	@echo "  run              - Build and run CLI mode"
	@echo "  run-all-regions  - Build and run with --all-regions"
	@echo "  tidy             - Tidy go modules"
	@echo "  lint             - Run golangci-lint (if installed)"
	@echo "  all              - fmt + vet + test + build + install"
