GOCACHE ?= $(CURDIR)/.gocache
BINARY ?= triage
INSTALL_DIR := $(shell sh -c 'gobin="$$(go env GOBIN)"; if [ -n "$$gobin" ]; then printf "%s" "$$gobin"; else printf "%s/bin" "$$(go env GOPATH)"; fi')

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
	@printf '\nInstalled %s to %s/%s\n' "$(BINARY)" "$(INSTALL_DIR)" "$(BINARY)"
	@printf 'If `%s` is not found, add this directory to your PATH:\n' "$(BINARY)"
	@printf '  export PATH="$$PATH:%s"\n\n' "$(INSTALL_DIR)"
