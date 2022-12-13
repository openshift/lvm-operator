# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

OPERATOR_SDK_VERSION ?= 1.23.0

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
BUNDLE_PACKAGE ?= lvms-operator

# Image URL to use all building/pushing image targets
IMAGE_REGISTRY ?= quay.io
REGISTRY_NAMESPACE ?= ocs-dev
IMAGE_TAG ?= latest
IMAGE_NAME ?= lvms-operator
VGMANAGER_IMAGE_NAME ?= vgmanager
MUST_GATHER_IMAGE_NAME ?= lvms-must-gather
MUST_GATHER_FULL_IMAGE_NAME=$(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(MUST_GATHER_IMAGE_NAME):$(IMAGE_TAG)

# IMG defines the image used for the operator.
IMG ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(IMAGE_NAME):$(IMAGE_TAG)
# VGMANAGER_IMG is the image for the vgmanager daemonset
VGMANAGER_IMG ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(VGMANAGER_IMAGE_NAME):$(IMAGE_TAG)
RBAC_PROXY_IMG ?= gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0


# BUNDLE_IMG defines the image used for the bundle.
BUNDLE_IMAGE_NAME ?= $(IMAGE_NAME)-bundle
BUNDLE_IMG ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(BUNDLE_IMAGE_NAME):$(IMAGE_TAG)


# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec


IMAGE_BUILD_CMD ?= $(shell command -v docker 2>&1 >/dev/null && echo docker || echo podman)


MANAGER_NAME_PREFIX ?= lvms-

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

godeps-update: ## Run go mod tidy and go mod vendor.
	go mod tidy && go mod vendor

test: manifests generate fmt vet envtest godeps-update ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -v -cover `go list ./... | grep -v "e2e"`


OPERATOR_NAMESPACE ?= openshift-storage
TOPOLVM_CSI_IMAGE ?= quay.io/ocs-dev/topolvm:latest
CSI_REGISTRAR_IMAGE ?= k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.5.1
CSI_PROVISIONER_IMAGE ?= k8s.gcr.io/sig-storage/csi-provisioner:v3.2.1
CSI_LIVENESSPROBE_IMAGE ?= k8s.gcr.io/sig-storage/livenessprobe:v2.7.0
CSI_RESIZER_IMAGE ?= k8s.gcr.io/sig-storage/csi-resizer:v1.5.0
CSI_SNAPSHOTTER_IMAGE ?= k8s.gcr.io/sig-storage/csi-snapshotter:v6.0.1
VGMANAGER_IMAGE ?= quay.io/ocs-dev/vgmanager:latest


define MANAGER_ENV_VARS
OPERATOR_NAMESPACE=$(OPERATOR_NAMESPACE)
TOPOLVM_CSI_IMAGE=$(TOPOLVM_CSI_IMAGE)
CSI_REGISTRAR_IMAGE=$(CSI_REGISTRAR_IMAGE)
CSI_PROVISIONER_IMAGE=$(CSI_PROVISIONER_IMAGE)
CSI_LIVENESSPROBE_IMAGE=$(CSI_LIVENESSPROBE_IMAGE)
CSI_RESIZER_IMAGE=$(CSI_RESIZER_IMAGE)
CSI_SNAPSHOTTER_IMAGE=$(CSI_SNAPSHOTTER_IMAGE)
endef
export MANAGER_ENV_VARS

update-mgr-env: ## Feed env variables to the manager configmap
	@echo "$$MANAGER_ENV_VARS" > config/manager/manager.env
	cp config/default/manager_custom_env.yaml.in config/default/manager_custom_env.yaml
	sed 's|TOPOLVM_CSI_IMAGE_VAL|$(TOPOLVM_CSI_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_LIVENESSPROBE_IMAGE_VAL|$(CSI_LIVENESSPROBE_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_PROVISIONER_IMAGE_VAL|$(CSI_PROVISIONER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_RESIZER_IMAGE_VAL|$(CSI_RESIZER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_REGISTRAR_IMAGE_VAL|$(CSI_REGISTRAR_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_SNAPSHOTTER_IMAGE_VAL|$(CSI_SNAPSHOTTER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml


##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

build-vgmanager: generate fmt vet ## Build vg manager binary.
	go build -o bin/vgmanager cmd/vgmanager/main.go

build-prometheus-alert-rules: jsonnet monitoring/mixin.libsonnet monitoring/alerts/alerts.jsonnet monitoring/alerts/*.libsonnet
	$(JSONNET) -S monitoring/alerts/alerts.jsonnet > config/prometheus/prometheus_rules.yaml

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: test ## Build docker image with the manager.
	$(IMAGE_BUILD_CMD) build -t ${IMG} .

docker-build-vgmanager: ## Build docker image with vgmanager.
	$(IMAGE_BUILD_CMD) build -f Dockerfile.vgmanager -t ${VGMANAGER_IMG} .

docker-build-combined: test ## Build docker image with manager and vgmanager
	$(IMAGE_BUILD_CMD) build -f Dockerfile.combined -t ${IMG} .

docker-push: ## Push docker image with the manager.
	$(IMAGE_BUILD_CMD) push ${IMG}

docker-push-vgmanager: ## Push docker image with the vgmanager.
	$(IMAGE_BUILD_CMD) push ${VGMANAGER_IMG}

docker-push-combined: docker-push ## Push docker image containing both manager and vgmanager binaries

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: update-mgr-env manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG} && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/default && $(KUSTOMIZE) edit set image rbac-proxy=$(RBAC_PROXY_IMG)
	cd config/webhook && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

deploy-with-olm: kustomize ## Deploy controller to the K8s cluster via OLM
	cd olm-deploy/ && $(KUSTOMIZE) edit set image catalog-img=${CATALOG_IMG}
	$(KUSTOMIZE) build olm-deploy/ | sed "s/lvms-operator.v.*/lvms-operator.v${VERSION}/g" | kubectl create -f -

undeploy-with-olm: ## Undeploy controller from the K8s cluster
	$(KUSTOMIZE) build olm-deploy/ | kubectl delete -f -


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.7.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.3)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

JSONNET = $(shell pwd)/bin/jsonnet
jsonnet: ## Download jsonnet locally if necessary.
	$(call go-get-tool,$(JSONNET),github.com/google/go-jsonnet/cmd/jsonnet@latest)

GINKGO = $(shell pwd)/bin/ginkgo
ginkgo: ## Download ginkgo and gomega locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo@v2)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || echo "Downloading $(2)"; GOBIN=$(PROJECT_DIR)/bin go install -mod=readonly $(2)
endef

.PHONY: rename-csv
rename-csv:
	cp config/manifests/bases/clusterserviceversion.yaml.in config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
	sed 's/@BUNDLE_PACKAGE@/$(BUNDLE_PACKAGE)/g' --in-place config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml

.PHONY: bundle
bundle: update-mgr-env manifests kustomize operator-sdk rename-csv build-prometheus-alert-rules ## Generate bundle manifests and metadata, then validate generated files.
	rm -rf bundle
#	$(OPERATOR_SDK) generate kustomize manifests --package $(BUNDLE_PACKAGE) -q
	cd config/default && $(KUSTOMIZE) edit set namespace $(OPERATOR_NAMESPACE)
	cd config/webhook && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG} && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/default && $(KUSTOMIZE) edit set image rbac-proxy=$(RBAC_PROXY_IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --package $(BUNDLE_PACKAGE) --version $(VERSION) $(BUNDLE_METADATA_OPTS) \
		--extra-service-accounts topolvm-node,vg-manager,topolvm-controller
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(IMAGE_BUILD_CMD) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.26.2/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: operator-sdk
OPERATOR_SDK = ./bin/operator-sdk
operator-sdk: ## Download operator-sdk locally
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_$${OS}_$${ARCH};\
	chmod +x $(OPERATOR_SDK) ;\

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# CATALOG_IMG defines the image used for the catalog.
CATALOG_IMAGE_NAME ?= $(IMAGE_NAME)-catalog
CATALOG_IMG ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(CATALOG_IMAGE_NAME):$(IMAGE_TAG)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool $(IMAGE_BUILD_CMD) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

lvms-must-gather:
	@echo "Building the lvms-must-gather image"
	$(IMAGE_BUILD_CMD) build -f must-gather/Dockerfile -t "${MUST_GATHER_FULL_IMAGE_NAME}" must-gather/

# Variables required to run and build LVM end to end tests.
LVM_OPERATOR_INSTALL ?= true
LVM_OPERATOR_UNINSTALL ?= true
SUBSCRIPTION_CHANNEL ?= alpha

# Handles only AWS as of now.
DISK_INSTALL ?= false

# Build and run lvm tests.
e2e-test: ginkgo
	@echo "build and run e2e tests"
	cd e2e && $(GINKGO) build
	cd e2e && ./e2e.test --lvm-catalog-image=$(CATALOG_IMG) --lvm-subscription-channel=$(SUBSCRIPTION_CHANNEL) --lvm-operator-install=$(LVM_OPERATOR_INSTALL) --lvm-operator-uninstall=$(LVM_OPERATOR_UNINSTALL) --disk-install=$(DISK_INSTALL) -ginkgo.v
