BINARY  := bin/aws_explorer
GO      := go
PKG     := ./...

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/ryandam9/aws_explorer/cmd.version=$(VERSION) \
           -X github.com/ryandam9/aws_explorer/cmd.commit=$(COMMIT) \
           -X github.com/ryandam9/aws_explorer/cmd.date=$(DATE)

.PHONY: all fmt vet test build clean run run-all-regions tidy lint man help

all: fmt vet test build

fmt:
	$(GO) fmt $(PKG)

vet:
	$(GO) vet $(PKG)

test:
	$(GO) test $(PKG) -v -count=1

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

clean:
	rm -f $(BINARY)
	rm -rf man

man: build
	./$(BINARY) docs --dir man

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
	@echo "  clean            - Remove binary and generated man pages"
	@echo "  man              - Generate man pages into ./man"
	@echo "  run              - Build and run CLI mode"
	@echo "  run-all-regions  - Build and run with --all-regions"
	@echo "  tidy             - Tidy go modules"
	@echo "  lint             - Run golangci-lint (if installed)"
	@echo "  all              - fmt + vet + test + build"
