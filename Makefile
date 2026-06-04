BINARY  := bin/aws_explorer
GO      := go
PKG     := ./...

.PHONY: all fmt vet test build clean run run-all-regions tidy lint help

all: fmt vet test build

fmt:
	$(GO) fmt $(PKG)

vet:
	$(GO) vet $(PKG)

test:
	$(GO) test $(PKG) -v -count=1

build:
	$(GO) build -o $(BINARY) main.go

clean:
	rm -f $(BINARY)

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
	@echo "  clean            - Remove binary"
	@echo "  run              - Build and run CLI mode"
	@echo "  run-all-regions  - Build and run with --all-regions"
	@echo "  tidy             - Tidy go modules"
	@echo "  lint             - Run golangci-lint (if installed)"
	@echo "  all              - fmt + vet + test + build"
