# Copyright (c) 2020 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
export PATH=$(shell echo $$PATH):$(PWD)/bin
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(PATH):$(GOBIN)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)

.PHONY: fmt lint fmt-dependencies lint-dependencies test

include build/common/Makefile.common.mk

# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

############################################################
# format section
############################################################

fmt-dependencies:
	$(call go-get-tool,$(PWD)/bin/gci,github.com/daixiang0/gci@v0.2.9)
	$(call go-get-tool,$(PWD)/bin/gofumpt,mvdan.cc/gofumpt@v0.2.0)

fmt: fmt-dependencies
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofmt -s -w
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gci -w -local "$(shell cat go.mod | head -1 | cut -d " " -f 2)"
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofumpt -l -w

############################################################
# lint section
############################################################

lint-dependencies:
	$(call go-get-tool,$(PWD)/bin/golangci-lint,github.com/golangci/golangci-lint/cmd/golangci-lint@v1.41.1)

lint: lint-dependencies lint-all

############################################################
# test section
############################################################

test:
	@go test $(TESTARGS) ./...
