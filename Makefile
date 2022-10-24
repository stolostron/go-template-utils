# Copyright (c) 2020 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23.x

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(LOCAL_BIN):$(GOBIN):$(PATH)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)

include build/common/Makefile.common.mk

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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

ENVTEST = $(LOCAL_BIN)/setup-envtest
GOSEC = $(LOCAL_BIN)/gosec

.PHONY: test
test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test $(TESTARGS) ./...

.PHONY: test-coverage
test-coverage: TESTARGS = -v -json -cover -covermode=atomic -coverprofile=coverage.out
test-coverage: test

.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

.PHONY: gosec
gosec:
	$(call go-get-tool,github.com/securego/gosec/v2/cmd/gosec@v2.9.6)

.PHONY: gosec-scan
gosec-scan: gosec
	$(GOSEC) -fmt sonarqube -out gosec.json -no-fail -exclude-dir=.go ./...
