
SHELL := /usr/bin/env bash

NUM_CLUSTERS := 2
KCP_BRANCH := release-prototype-2

IMAGE_TAG_BASE ?= quay.io/kuadrant/kcp-glbc
IMAGE_TAG ?= latest
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)

KUBECONFIG ?= $(shell pwd)/.kcp/admin.kubeconfig
CLUSTERS_KUBECONFIG_DIR ?= $(shell pwd)/tmp

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: clean
clean: ## Clean up temporary files.
	-rm -rf ./.kcp
	-rm -f ./bin/*
	-rm -rf ./tmp

generate: generate-deepcopy generate-crd generate-client ## Generate code containing DeepCopy method implementations, CustomResourceDefinition objects and Clients.

generate-deepcopy: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) paths="./..." object

generate-crd: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) crd paths=./... output:crd:artifacts:config=../../../config/crd output:crd:dir=../../../config/crd crd:crdVersions=v1 && rm -rf ./config

generate-client:
	./scripts/gen_client.sh

vendor: ## Vendor the dependencies.
	go mod tidy
	go mod vendor
.PHONY: vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

lint: ## Run golangci-lint against code.
	golangci-lint run ./...
.PHONY: lint

.PHONY: test
test: generate ## Run tests.
	#ToDo Implement `test` target

e2e: build
	KUBECONFIG="$(KUBECONFIG)" CLUSTERS_KUBECONFIG_DIR="$(CLUSTERS_KUBECONFIG_DIR)" \
	go test -timeout 60m -v ./e2e -tags=e2e

##@ CI

#Note, these targets are expected to run in a clean CI environment.

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code

##@ Build

build: ## Build the project.
	go build -o bin ./cmd/...
.PHONY: build

.PHONY: docker-build
docker-build: ## Build docker image.
	docker build -t ${IMG} .

##@ Deployment

.PHONY: local-setup
local-setup: clean build kind kcp ## Setup kcp locally using kind.
	./utils/local-setup.sh -c ${NUM_CLUSTERS}

KCP = $(shell pwd)/bin/kcp
kcp: ## Download kcp locally.
	rm -rf ./tmp/kcp
	git clone --depth=1 --branch ${KCP_BRANCH} https://github.com/kuadrant/kcp ./tmp/kcp
	cd ./tmp/kcp && make
	cp ./tmp/kcp/bin/cluster-controller $(shell pwd)/bin
	cp ./tmp/kcp/bin/compat $(shell pwd)/bin
	cp ./tmp/kcp/bin/crd-puller $(shell pwd)/bin
	cp ./tmp/kcp/bin/deployment-splitter $(shell pwd)/bin
	cp ./tmp/kcp/bin/kcp $(shell pwd)/bin
	cp ./tmp/kcp/bin/kubectl-kcp $(shell pwd)/bin
	cp ./tmp/kcp/bin/shard-proxy $(shell pwd)/bin
	cp ./tmp/kcp/bin/syncer $(shell pwd)/bin
	cp ./tmp/kcp/bin/virtual-workspaces $(shell pwd)/bin
	rm -rf ./tmp/kcp

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KIND = $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
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
