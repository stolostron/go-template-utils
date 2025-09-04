# Copyright (c) 2020 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.x

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(LOCAL_BIN):$(GOBIN):$(PATH)

include build/common/Makefile.common.mk

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

.PHONY: fmt
fmt:

############################################################
# lint section
############################################################

.PHONY: lint
lint:

############################################################
# test section
############################################################

.PHONY: test
test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test $(TESTARGS) -coverpkg=$(shell cat go.mod | head -1 | cut -d ' ' -f 2)/... ./...

.PHONY: test-coverage
test-coverage: TESTARGS = -cover -covermode=atomic -coverprofile=coverage.out
test-coverage: test

.PHONY: gosec-scan
gosec-scan:

.PHONY: testdata/crds.yaml
testdata/crds.yaml: kustomize
	$(KUSTOMIZE) build testdata/crds-kustomize > testdata/crds.yaml
