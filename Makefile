include make/license.mk

# Setting SHELL to bash allows bash commands to be executed by recipes. 
# This is a requirement for 'setup-envtest.sh' in the test target. 
# Options are set to exit when a recipe line exits non-zero or a piped command fails. 
SHELL = /usr/bin/env bash -o pipefail 
.SHELLFLAGS = -ec 
CURPATH=$(PWD)
TARGET_DIR=$(CURPATH)/build/_output
BIN_DIR=$(CURPATH)/bin
KUBECONFIG?=$(HOME)/.kube/config
export OPERATOR_EXEC?=oc

BUILD_GOPATH=$(TARGET_DIR):$(TARGET_DIR)/vendor:$(CURPATH)/cmd
IMAGE_BUILDER?=docker
IMAGE_BUILD_OPTS?=
DOCKERFILE?=Dockerfile
DOCKERFILE_CONFIG_DAEMON?=Dockerfile.sriov-network-config-daemon
DOCKERFILE_WEBHOOK?=Dockerfile.webhook

CRD_BASES=./config/crd/bases

export APP_NAME?=sriov-network-operator
TARGET=$(TARGET_DIR)/bin/$(APP_NAME)
IMAGE_REPO?=ghcr.io/k8snetworkplumbingwg
IMAGE_TAG?=$(IMAGE_REPO)/$(APP_NAME):latest
CONFIG_DAEMON_IMAGE_TAG?=$(IMAGE_REPO)/$(APP_NAME)-config-daemon:latest
WEBHOOK_IMAGE_TAG?=$(IMAGE_REPO)/$(APP_NAME)-webhook:latest
MAIN_PKG=cmd/manager/main.go
export NAMESPACE?=openshift-sriov-network-operator
export WATCH_NAMESPACE?=openshift-sriov-network-operator
export HOME?=$(PWD)
export GOPATH?=$(shell go env GOPATH)
export GO111MODULE=on
PKGS=$(shell go list ./... | grep -v -E '/vendor/|/test|/examples')
TESTPKGS?=./...

# go source files, ignore vendor directory
SRC = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:crdVersions={v1}"

GOLANGCI_LINT = $(BIN_DIR)/golangci-lint
# golangci-lint version should be updated periodically
# we keep it fixed to avoid it from unexpectedly failing on the project
# in case of a version bump
GOLANGCI_LINT_VER = v1.64.7

.PHONY: all build clean gendeepcopy test test-e2e test-e2e-k8s run image fmt sync-manifests test-e2e-conformance manifests update-codegen

all: generate lint build

build: manager _build-sriov-network-config-daemon _build-webhook _build-sriov-network-operator-config-cleanup

_build-%:
	WHAT=$* hack/build-go.sh

clean:
	@rm -rf $(TARGET_DIR)
	@rm -rf $(BIN_DIR)

image: ; $(info Building images...)
	$(IMAGE_BUILDER) build -f $(DOCKERFILE) -t $(IMAGE_TAG) $(CURPATH) $(IMAGE_BUILD_OPTS)
	$(IMAGE_BUILDER) build -f $(DOCKERFILE_CONFIG_DAEMON) -t $(CONFIG_DAEMON_IMAGE_TAG) $(CURPATH) $(IMAGE_BUILD_OPTS)
	$(IMAGE_BUILDER) build -f $(DOCKERFILE_WEBHOOK) -t $(WEBHOOK_IMAGE_TAG) $(CURPATH) $(IMAGE_BUILD_OPTS)

# Run tests
test: generate lint manifests envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir=/tmp -p path)" HOME="$(shell pwd)" go test -coverprofile cover.out -v ${TESTPKGS}

# Build manager binary
manager: generate _build-manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: skopeo install
	hack/run-locally.sh

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
# deploy: manifests kustomize
# 	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
# 	$(KUSTOMIZE) build config/default | kubectl apply -f -

# UnDeploy controller from the configured Kubernetes cluster in ~/.kube/config
# undeploy:
# 	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) webhook paths="./..." output:crd:artifacts:config=$(CRD_BASES)
	cp ./config/crd/bases/* ./deployment/sriov-network-operator-chart/crds/

check-manifests: manifests
	@set +e; git diff --quiet config; \
	if [ $$? -eq 1 ]; \
	then echo -e "\n`config` folder is out of date. Please run `make manifests` and commit your changes"; \
	exit 1; fi

sync-manifests-%: manifests
	@mkdir -p manifests/$*
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_sriovibnetworks.yaml | awk 'NF' > manifests/$*/sriov-network-operator-sriovibnetworks_crd.yaml
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_sriovnetworknodepolicies.yaml | awk 'NF' > manifests/$*/sriov-network-operator-sriovnetworknodepolicy.crd.yaml
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_sriovnetworknodestates.yaml | awk 'NF' > manifests/$*/sriov-network-operator-sriovnetworknodestate.crd.yaml
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_sriovoperatorconfigs.yaml | awk 'NF' > manifests/$*/sriov-network-operator-sriovoperatorconfig.crd.yaml
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_sriovnetworks.yaml | awk 'NF' > manifests/$*/sriov-network-operator-sriovnetwork.crd.yaml
	sed '2{/---/d}' $(CRD_BASES)/sriovnetwork.openshift.io_ovsnetworks.yaml | awk 'NF' > manifests/$*/sriov-network-operator-ovsnetwork.yaml
	@echo ""
	@echo "*************************************************************************************************************************************************"
	@echo "* Please manually update the sriov-network-operator.v4.7.0.clusterserviceversion.yaml and image-references files in the manifests/$* directory *"
	@echo "*************************************************************************************************************************************************"
	@echo ""


# Run go fmt against code

fmt: ## Go fmt your code
	CONTAINER_CMD=$(IMAGE_BUILDER) hack/go-fmt.sh .

# Run go fmt against code
fmt-code:
	go fmt ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

mock-generate: gomock
	go generate ./...

CONTROLLER_GEN = $(BIN_DIR)/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0)

KUSTOMIZE = $(BIN_DIR)/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.5)

ENVTEST = $(BIN_DIR)/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.16)

GOMOCK = $(shell pwd)/bin/mockgen
gomock:
	$(call go-install-tool,$(GOMOCK),go.uber.org/mock/mockgen@v0.5.0)

GINKGO = $(BIN_DIR)/ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo)

# go-install-tool will 'go install' any package $2 and install it to $1.
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
GOBIN=$(BIN_DIR) go install $(2) ;\
}
endef

skopeo:
	if ! which skopeo; then if [ -z ${SKIP_VAR_SET} ]; then if [ -f /etc/redhat-release ]; then dnf -y install skopeo; elif [ -f /etc/lsb-release ]; then sudo apt-get -y update; sudo apt-get -y install skopeo; fi; fi; fi

fakechroot:
	if ! which fakechroot; then if [ -f /etc/redhat-release ]; then dnf -y install fakechroot; elif [ -f /etc/lsb-release ]; then sudo apt-get -y update; sudo apt-get -y install fakechroot; fi; fi

$(BIN_DIR)/helm helm:
	mkdir -p $(BIN_DIR)
	curl -fsSL -o $(BIN_DIR)/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
	chmod 700 $(BIN_DIR)/get_helm.sh
	HELM_INSTALL_DIR=$(BIN_DIR) $(BIN_DIR)/get_helm.sh

deploy-setup: export ADMISSION_CONTROLLERS_ENABLED?=false
deploy-setup: skopeo install
	hack/deploy-setup.sh $(NAMESPACE)

deploy-setup-k8s: export NAMESPACE=sriov-network-operator
deploy-setup-k8s: export CNI_BIN_PATH=/opt/cni/bin
deploy-setup-k8s: export OPERATOR_EXEC=kubectl
deploy-setup-k8s: export CLUSTER_TYPE=kubernetes
deploy-setup-k8s: deploy-setup

test-e2e-conformance: ginkgo
	SUITE=./test/conformance ./hack/run-e2e-conformance.sh

test-e2e-conformance-virtual-k8s-cluster-ci: ginkgo
	./hack/run-e2e-conformance-virtual-cluster.sh

test-e2e-conformance-virtual-k8s-cluster: ginkgo
	SKIP_DELETE=TRUE ./hack/run-e2e-conformance-virtual-cluster.sh

test-e2e-conformance-virtual-ocp-cluster-ci: ginkgo
	./hack/run-e2e-conformance-virtual-ocp.sh

test-e2e-conformance-virtual-ocp-cluster: ginkgo
	SKIP_DELETE=TRUE ./hack/run-e2e-conformance-virtual-ocp.sh

redeploy-operator-virtual-cluster:
	./hack/virtual-cluster-redeploy.sh

test-e2e-validation-only: ginkgo
	SUITE=./test/validation ./hack/run-e2e-conformance.sh	

test-e2e: generate manifests skopeo envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir=/tmp -p path)"; source hack/env.sh; HOME="$(shell pwd)" go test ./test/e2e/... -timeout 60m -coverprofile cover.out -v

test-e2e-k8s: export NAMESPACE=sriov-network-operator
test-e2e-k8s: test-e2e

test-bindata-scripts: fakechroot
	fakechroot ./test/scripts/kargs_test.sh

test-%: generate manifests envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir=/tmp -p path)" HOME="$(shell pwd)" go test `go list ./$*/... | grep -v "/mock" | grep -v "/pkg/client"` -coverprofile cover-$*-$(CLUSTER_TYPE).out -coverpkg ./... -v

GOCOVMERGE = $(BIN_DIR)/gocovmerge
gocovmerge: ## Download gocovmerge locally if necessary.
	$(call go-install-tool,$(GOCOVMERGE),github.com/shabbyrobe/gocovmerge/cmd/gocovmerge@latest)

GCOV2LCOV = $(BIN_DIR)/gcov2lcov
gcov2lcov:
	$(call go-install-tool,$(GCOV2LCOV),github.com/jandelgado/gcov2lcov@v1.0.5)

merge-test-coverage: gocovmerge gcov2lcov
	$(GOCOVMERGE) cover-*.out > cover.out
	$(GCOV2LCOV) -infile cover.out -outfile lcov.out

deploy-wait:
	hack/deploy-wait.sh

undeploy: uninstall
	@hack/undeploy.sh $(NAMESPACE)

undeploy-k8s: export NAMESPACE=sriov-network-operator
undeploy-k8s: export OPERATOR_EXEC=kubectl
undeploy-k8s: undeploy

deps-update:
	go mod tidy

check-deps: deps-update
	@set +e; git diff --quiet HEAD go.sum go.mod; \
	if [ $$? -eq 1 ]; \
	then echo -e "\ngo modules are out of date. Please commit after running 'make deps-update' command\n"; \
	exit 1; fi

$(GOLANGCI_LINT): ; $(info installing golangci-lint...)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VER))

.PHONY: lint
lint: | $(GOLANGCI_LINT) ; $(info  running golangci-lint...) @ ## Run golangci-lint
	$(GOLANGCI_LINT) run --timeout=10m

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

YQ=$(BIN_DIR)/yq
YQ_VERSION=v4.44.1
$(YQ): | $(BIN_DIR); $(info installing yq)
	@curl -fsSL -o $(YQ) https://github.com/mikefarah/yq/releases/download/$(YQ_VERSION)/yq_linux_amd64 && chmod +x $(YQ)

.PHONY: chart-prepare-release
chart-prepare-release: | $(YQ) ; ## prepare chart for release
	@GITHUB_TAG=$(GITHUB_TAG) GITHUB_TOKEN=$(GITHUB_TOKEN) GITHUB_REPO_OWNER=$(GITHUB_REPO_OWNER) hack/release/chart-update.sh

.PHONY: chart-push-release
chart-push-release: ## push release chart
	@GITHUB_TAG=$(GITHUB_TAG) GITHUB_TOKEN=$(GITHUB_TOKEN) GITHUB_REPO_OWNER=$(GITHUB_REPO_OWNER) hack/release/chart-push.sh
