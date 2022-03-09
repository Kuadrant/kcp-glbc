all: vendor build
.PHONY: all

SHELL := /bin/bash
NUM_CLUSTERS := 2
KCP_BRANCH := release-prototype-2
# go-get-tool will 'go get' any package $2 and install it to $1.
# backing up and recovering the go.mod/go.sum as go install doesnt work
# with project that use replacement directives, no time for a nicer solution.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
mkdir -p $(PROJECT_DIR)/tmp ;\
cp $(PROJECT_DIR)/{go.mod,go.sum} $(PROJECT_DIR)/tmp ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
cp $(PROJECT_DIR)/tmp/{go.mod,go.sum} $(PROJECT_DIR)/ ;\
}
endef

build:
	go build -o bin ./cmd/...
.PHONY: build

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

KIND = $(shell pwd)/bin/kind
kind:
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

# Not ideal, fix when possible.
KCP = $(shell pwd)/bin/kcp
kcp:
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

.PHONY: local-setup
local-setup: clean build kind kcp
	./utils/local-setup.sh -c ${NUM_CLUSTERS}

.PHONY: clean
clean:
	-rm -rf ./.kcp
	-rm -f ./bin/*
	-rm -rf ./tmp

generate: generate-deepcopy generate-crd generate-client

generate-deepcopy: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) paths="./..." object

generate-crd: controller-gen
	cd pkg/apis/kuadrant && $(CONTROLLER_GEN) crd paths=./... output:crd:artifacts:config=../../../config/crd output:crd:dir=../../../config/crd crd:crdVersions=v1 && rm -rf ./config

generate-client:
	./scripts/gen_client.sh

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

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
