# Copyright (c) 2020 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(PATH):$(GOBIN)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)

.PHONY: fmt lint lint-dependencies test

include build/common/Makefile.common.mk

############################################################
# format section
############################################################

fmt:
	go fmt ./...

############################################################
# lint section
############################################################

lint-dependencies:
	@if [ ! -f $(GOBIN)/golangci-lint ]; then\
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v1.41.1/install.sh | sh -s -- -b $(GOBIN) v1.41.1;\
    fi

lint: lint-dependencies lint-all

############################################################
# test section
############################################################
GOSEC = $(GOBIN)/gosec

test:
	go test $(TESTARGS) ./...

.PHONY: test-coverage
test-coverage: TESTARGS = -v -json -cover -covermode=atomic -coverprofile=coverage.out
test-coverage: test

.PHONY: gosec
gosec:
	go install github.com/securego/gosec/v2/cmd/gosec@v2.9.6

.PHONY: gosec-scan
gosec-scan: gosec
	$(GOSEC) -fmt sonarqube -out gosec.json -no-fail -exclude-dir=.go ./...
