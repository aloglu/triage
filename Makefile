GOCACHE ?= $(CURDIR)/.gocache
BINARY ?= triage

.PHONY: run build test install

run:
	GOCACHE=$(GOCACHE) go run ./cmd/triage

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -o bin/$(BINARY) ./cmd/triage

test:
	GOCACHE=$(GOCACHE) go test ./...

install:
	go install ./cmd/triage
