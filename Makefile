GOCACHE ?= $(CURDIR)/.gocache
BINARY ?= triage
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: run build test check install

run:
	GOCACHE=$(GOCACHE) go run ./cmd/triage

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/triage

test:
	GOCACHE=$(GOCACHE) go test ./...

# check runs everything CI runs.
check:
	GOCACHE=$(GOCACHE) go vet ./...
	GOCACHE=$(GOCACHE) go test -race ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/triage
	@echo "Installed. If 'triage' isn't found, add $$(go env GOPATH)/bin to your PATH."
