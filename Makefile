# Copyright 2019 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
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

.PHONY: copyright-check fmt lint lint-dependencies test


include build/common/Makefile.common.mk

############################################################
# work section
############################################################
$(GOBIN):
	@echo "create gobin"
	@mkdir -p $(GOBIN)

work: $(GOBIN)

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

test:
	@go test $(TESTARGS) ./...

############################################################
# check copyright section
############################################################
copyright-check:
	./build/copyright-check.sh $(TRAVIS_BRANCH)
