SHELL := /bin/bash
GO := GO111MODULE=on GO15VENDOREXPERIMENT=1 go
GO_NOMOD := GO111MODULE=off go
GO_VERSION       := $(shell $(GO) version | sed -e 's/^[^0-9.]*\([0-9.]*\).*/\1/')

export PATH := $(PATH):$(GOPATH1)/bin

build: clean dependencies
	$(GO) build -mod=vendor -ldflags="-w -s" -v -o action

dependencies:
	$(GO) get -d -v

clean:
	go clean -i && rm -f action