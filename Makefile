# Copyright (c) 2020 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(LOCAL_BIN):$(GOBIN):$(PATH)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)

include build/common/Makefile.common.mk

# go-get-tool will 'go install' any package $1 and install it to LOCAL_BIN.
define go-get-tool
@set -e ;\
echo "Checking installation of $(1)" ;\
GOBIN=$(LOCAL_BIN) go install $(1)
endef

############################################################
# clean section
############################################################

.PHONY: clean
clean:
	-rm bin/*
	-rm -r vendor/

############################################################
# format section
############################################################

fmt-dependencies:
	$(call go-get-tool,github.com/daixiang0/gci@v0.6.0)
	$(call go-get-tool,mvdan.cc/gofumpt@v0.3.1)

fmt: fmt-dependencies
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofmt -s -w
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gci write -s standard -s default -s "prefix($(shell cat go.mod | head -1 | cut -d " " -f 2))"
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofumpt -l -w

############################################################
# lint section
############################################################

lint-dependencies:
	$(call go-get-tool,github.com/golangci/golangci-lint/cmd/golangci-lint@v1.47.3)

lint: lint-dependencies lint-all

############################################################
# test section
############################################################

test:
	go test $(TESTARGS) ./...
