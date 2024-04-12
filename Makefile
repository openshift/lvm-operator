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

UNAME := $(shell uname)

## Versions

# OPERATOR_VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the OPERATOR_VERSION as arg of the bundle target (e.g make bundle OPERATOR_VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export OPERATOR_VERSION=0.0.2)
OPERATOR_VERSION ?= 0.0.1

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

OPERATOR_SDK_VERSION ?= 1.32.0

MANAGER_NAME_PREFIX ?= lvms-
OPERATOR_NAMESPACE ?= openshift-storage
TOPOLVM_CSI_IMAGE ?= quay.io/lvms_dev/topolvm:latest
RBAC_PROXY_IMAGE ?= gcr.io/kubebuilder/kube-rbac-proxy:v0.15.0
CSI_REGISTRAR_IMAGE ?= k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.7.0
CSI_PROVISIONER_IMAGE ?= k8s.gcr.io/sig-storage/csi-provisioner:v3.4.1
CSI_LIVENESSPROBE_IMAGE ?= k8s.gcr.io/sig-storage/livenessprobe:v2.9.0
CSI_RESIZER_IMAGE ?= k8s.gcr.io/sig-storage/csi-resizer:v1.7.0
CSI_SNAPSHOTTER_IMAGE ?= k8s.gcr.io/sig-storage/csi-snapshotter:v6.2.1

define MANAGER_ENV_VARS
OPERATOR_NAMESPACE=$(OPERATOR_NAMESPACE)
TOPOLVM_CSI_IMAGE=$(TOPOLVM_CSI_IMAGE)
RBAC_PROXY_IMAGE=$(RBAC_PROXY_IMAGE)
CSI_REGISTRAR_IMAGE=$(CSI_REGISTRAR_IMAGE)
CSI_PROVISIONER_IMAGE=$(CSI_PROVISIONER_IMAGE)
CSI_LIVENESSPROBE_IMAGE=$(CSI_LIVENESSPROBE_IMAGE)
CSI_RESIZER_IMAGE=$(CSI_RESIZER_IMAGE)
CSI_SNAPSHOTTER_IMAGE=$(CSI_SNAPSHOTTER_IMAGE)
endef
export MANAGER_ENV_VARS

update-mgr-env: ## Feed environment variables to the manager ConfigMap.
	@echo "$$MANAGER_ENV_VARS" > config/manager/manager.env
	cp config/default/manager_custom_env.yaml.in config/default/manager_custom_env.yaml
ifeq ($(UNAME), Darwin)
	sed -i '' 's|TOPOLVM_CSI_IMAGE_VAL|$(TOPOLVM_CSI_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|RBAC_PROXY_IMAGE_VAL|$(RBAC_PROXY_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|CSI_LIVENESSPROBE_IMAGE_VAL|$(CSI_LIVENESSPROBE_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|CSI_PROVISIONER_IMAGE_VAL|$(CSI_PROVISIONER_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|CSI_RESIZER_IMAGE_VAL|$(CSI_RESIZER_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|CSI_REGISTRAR_IMAGE_VAL|$(CSI_REGISTRAR_IMAGE)|g' config/default/manager_custom_env.yaml
	sed -i '' 's|CSI_SNAPSHOTTER_IMAGE_VAL|$(CSI_SNAPSHOTTER_IMAGE)|g' config/default/manager_custom_env.yaml
else
	sed 's|TOPOLVM_CSI_IMAGE_VAL|$(TOPOLVM_CSI_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|RBAC_PROXY_IMAGE_VAL|$(RBAC_PROXY_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_LIVENESSPROBE_IMAGE_VAL|$(CSI_LIVENESSPROBE_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_PROVISIONER_IMAGE_VAL|$(CSI_PROVISIONER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_RESIZER_IMAGE_VAL|$(CSI_RESIZER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_REGISTRAR_IMAGE_VAL|$(CSI_REGISTRAR_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
	sed 's|CSI_SNAPSHOTTER_IMAGE_VAL|$(CSI_SNAPSHOTTER_IMAGE)|g' --in-place config/default/manager_custom_env.yaml
endif

## Variables for the images

# Image URL to use all building/pushing image targets
IMAGE_REGISTRY ?= quay.io
REGISTRY_NAMESPACE ?= lvms_dev
IMAGE_TAG ?= latest
IMAGE_NAME ?= lvms-operator
IMAGE_REPO ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(IMAGE_NAME)
# IMG defines the image used for the operator.
IMG ?= $(IMAGE_REPO):$(IMAGE_TAG)

# MUST_GATHER_IMG defines the image used for the must-gather.
MUST_GATHER_IMAGE_NAME ?= lvms-must-gather
MUST_GATHER_IMG=$(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(MUST_GATHER_IMAGE_NAME):$(IMAGE_TAG)

## Variables for the bundle

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

# BUNDLE_IMG defines the image used for the bundle.
BUNDLE_IMAGE_NAME ?= $(IMAGE_NAME)-bundle
BUNDLE_REPO ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(BUNDLE_IMAGE_NAME)
BUNDLE_IMG ?= $(BUNDLE_REPO):$(IMAGE_TAG)

# Each CSV has a replaces parameter that indicates which Operator it replaces.
# This builds a graph of CSVs that can be queried by OLM, and updates can be
# shared between channels. Channels can be thought of as entry points into
# the graph of updates:
REPLACES ?=

# Creating the New CatalogSource requires publishing CSVs that replace one Operator,
# but can skip several. This can be accomplished using the skipRange annotation:
SKIP_RANGE ?=

## Variables for the catalog

# CATALOG_IMG defines the image used for the catalog.
CATALOG_IMAGE_NAME ?= $(IMAGE_NAME)-catalog
CATALOG_REPO ?= $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/$(CATALOG_IMAGE_NAME)
CATALOG_IMG ?= $(CATALOG_REPO):$(IMAGE_TAG)
CATALOG_DIR ?= ./catalog

define CATALOG_CHANNEL
---
schema: olm.channel
package: $(IMAGE_NAME)
name: alpha
entries:
  - name: lvms-operator.v$(OPERATOR_VERSION)
endef
export CATALOG_CHANNEL

define CATALOG_PACKAGE
---
schema: olm.package
name: $(IMAGE_NAME)
defaultChannel: alpha
endef
export CATALOG_PACKAGE

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

mocks: mockery ## Generate mocks for unit test code
	$(shell $(MOCKERY) --log-level error)

generate: controller-gen mocks ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations. Also retriggers mock generation
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

godeps-update: ## Run go mod tidy and go mod vendor.
	go mod tidy && go mod vendor

verify: ## Verify go formatting and generated files.
	hack/verify-gofmt.sh
	hack/verify-deps.sh
	hack/verify-bundle.sh
	hack/verify-catalog.sh
	hack/verify-generated.sh

test: manifests generate envtest godeps-update ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -v -coverprofile=coverage.out `go list ./... | grep -v -e "e2e" -e "performance"`
ifeq ($(OPENSHIFT_CI), true)
	hack/publish-codecov.sh
endif

run: manifests generate ## Run the Operator from your host.
	go run ./main.go

##@ Build

IMAGE_BUILD_CMD ?= $(shell command -v docker 2>&1 >/dev/null && echo docker || echo podman)
OS ?= linux
ARCH ?= amd64

all: build

build: generate fmt vet ## Build manager binary.
	GOOS=$(OS) GOARCH=$(ARCH) go build -gcflags='all=-N -l' -o bin/lvms cmd/main.go

build-prometheus-alert-rules: jsonnet monitoring/mixin.libsonnet monitoring/alerts/alerts.jsonnet monitoring/alerts/*.libsonnet
	$(JSONNET) -S monitoring/alerts/alerts.jsonnet > config/prometheus/prometheus_rules.yaml

docker-build: ## Build docker image with the manager.
	$(IMAGE_BUILD_CMD) build --platform=${OS}/${ARCH} -t ${IMG} .

docker-build-debug: ## Build remote-debugging enabled docker image with the manager. See CONTRIBUTING.md for more information
	$(IMAGE_BUILD_CMD) build -f hack/debug.Dockerfile --platform=${OS}/${ARCH} -t ${IMG} .

docker-push: ## Push docker image with the manager.
	$(IMAGE_BUILD_CMD) push ${IMG}

lvms-must-gather:
	@echo "Building the lvms-must-gather image"
	$(IMAGE_BUILD_CMD) build -f must-gather/Dockerfile -t "${MUST_GATHER_IMG}" must-gather/

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: update-mgr-env manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG} && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/webhook && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

deploy-debug: update-mgr-env manifests kustomize ## Deploy controller started through delve to the K8s cluster specified in ~/.kube/config. See CONTRIBUTING.md for more information
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG} && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/webhook && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	$(KUSTOMIZE) build config/debug | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

deploy-with-olm: kustomize ## Deploy controller to the K8s cluster via OLM.
	cd config/olm-deploy && $(KUSTOMIZE) edit set image catalog-img=${CATALOG_IMG}
	$(KUSTOMIZE) build config/olm-deploy | sed "s/lvms-operator.v.*/lvms-operator.v${OPERATOR_VERSION}/g" | kubectl create -f -

undeploy-with-olm: ## Undeploy controller from the K8s cluster.
	$(KUSTOMIZE) build config/olm-deploy | kubectl delete -f -

##@ Bundle image

.PHONY: rename-csv
rename-csv:
	cp config/manifests/bases/clusterserviceversion.yaml.in config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
ifeq ($(UNAME), Darwin)
	sed -i '' 's/@BUNDLE_PACKAGE@/$(BUNDLE_PACKAGE)/g' config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
else
	sed 's/@BUNDLE_PACKAGE@/$(BUNDLE_PACKAGE)/g' --in-place config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
endif

.PHONY: bundle
bundle: update-mgr-env manifests kustomize operator-sdk rename-csv build-prometheus-alert-rules ## Generate bundle manifests and metadata, then validate generated files.
	rm -rf bundle
#	$(OPERATOR_SDK) generate kustomize manifests --package $(BUNDLE_PACKAGE) -q
	cd config/default && $(KUSTOMIZE) edit set namespace $(OPERATOR_NAMESPACE)
	cd config/webhook && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG} && $(KUSTOMIZE) edit set nameprefix ${MANAGER_NAME_PREFIX}
	cd config/manifests/bases && \
		rm -rf kustomization.yaml && \
		$(KUSTOMIZE) create --resources $(BUNDLE_PACKAGE).clusterserviceversion.yaml && \
		$(KUSTOMIZE) edit add annotation --force 'olm.skipRange':"$(SKIP_RANGE)" && \
		$(KUSTOMIZE) edit add patch --name $(BUNDLE_PACKAGE).v0.0.0 --kind ClusterServiceVersion\
		--patch '[{"op": "replace", "path": "/spec/replaces", "value": "$(REPLACES)"}]'
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --package $(BUNDLE_PACKAGE) --version $(OPERATOR_VERSION) $(BUNDLE_METADATA_OPTS) \
		--extra-service-accounts topolvm-node,vg-manager,topolvm-controller
	# now we remove the createdAt annotation as otherwise our change detection will say that a the bundle changed during verification
ifeq ($(UNAME), Darwin)
		sed -i '' -e '/createdAt/d' ./bundle/manifests/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
else
		sed -i '/createdAt/d' ./bundle/manifests/$(BUNDLE_PACKAGE).clusterserviceversion.yaml
endif
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(IMAGE_BUILD_CMD) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

##@ Catalog image
.PHONY: catalog
catalog: ## Render a catalog from the bundle by wrapping it in a alpha channel.
	@echo "Rendering the catalog at $(CATALOG_DIR)"
	@rm -rf $(CATALOG_DIR)
	@mkdir -p $(CATALOG_DIR)/$(IMAGE_NAME)
	@echo "$$CATALOG_CHANNEL" > $(CATALOG_DIR)/channel.yaml
	@echo "$$CATALOG_PACKAGE" > $(CATALOG_DIR)/package.yaml
	$(OPM) render ./bundle -oyaml > $(CATALOG_DIR)/$(IMAGE_NAME)/v$(OPERATOR_VERSION).yaml
	$(OPM) validate $(CATALOG_DIR)

.PHONY: catalog-build
catalog-build: opm catalog ## Build a catalog image from the rendered catalog.
	$(IMAGE_BUILD_CMD) build -f catalog.Dockerfile -t $(CATALOG_IMG) .

.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ E2E tests

# Variables required to run and build LVM end to end tests.
LVM_OPERATOR_INSTALL ?= false
LVM_OPERATOR_UNINSTALL ?= false
SUBSCRIPTION_CHANNEL ?= alpha

# Handles only AWS as of now.
DISK_INSTALL ?= false

.PHONY: e2e
e2e: ginkgo ## Build and run e2e tests.
	cd test/e2e && $(GINKGO) build
	cd test/e2e && ./e2e.test --lvm-catalog-image=$(CATALOG_IMG) --lvm-subscription-channel=$(SUBSCRIPTION_CHANNEL) --lvm-operator-install=$(LVM_OPERATOR_INSTALL) --lvm-operator-uninstall=$(LVM_OPERATOR_UNINSTALL) --disk-install=$(DISK_INSTALL) -ginkgo.v

performance-stress-test: ## Build and run stress tests. Requires a fully setup LVMS installation. if you receive an error during running because of a missing token it might be because you have not logged in via token authentication but OIDC. you need a token login to run the performance test.
	oc apply -f ./config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage
	go run ./test/performance -t $(oc whoami -t) -s lvms-vg1 -i 64
	oc delete -f ./config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage --cascade=foreground --wait

performance-idle-test: ## Build and run idle tests. Requires a fully setup LVMS installation. if you receive an error during running because of a missing token it might be because you have not logged in via token authentication but OIDC. you need a token login to run the performance test.
	oc apply -f ./config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage
	go run ./test/performance -t $(oc whoami -t) --run-stress false --long-term-observation-window=30m
	oc delete -f ./config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage --cascade=foreground --wait

##@ Tools

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.13.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5@v5.2.1)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

JSONNET = $(shell pwd)/bin/jsonnet
jsonnet: ## Download jsonnet locally if necessary.
	$(call go-get-tool,$(JSONNET),github.com/google/go-jsonnet/cmd/jsonnet@latest)

GINKGO = $(shell pwd)/bin/ginkgo
ginkgo: ## Download ginkgo and gomega locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo@v2.13.0)

MOCKERY = $(shell pwd)/bin/mockery
mockery: ## Download mockery and add locally if necessary
	$(call go-get-tool,$(MOCKERY),github.com/vektra/mockery/v2@v2.36.0)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || echo "Downloading $(2)"; GOBIN=$(PROJECT_DIR)/bin go install -mod=readonly $(2)
endef

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.39.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: operator-sdk
OPERATOR_SDK = ./bin/operator-sdk
operator-sdk: ## Download operator-sdk locally.
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_$${OS}_$${ARCH};\
	chmod +x $(OPERATOR_SDK) ;\

.PHONY: git-sanitize
git-sanitize:
	hack/git-sanitize.sh

.PHONY: git-unsanitize
git-unsanitize:
	CLEANUP="true" hack/git-sanitize.sh

.PHONY: release-local-operator
release-local-operator:
	IMAGE_REPO=$(IMAGE_REPO) hack/release-local.sh

.PHONY: deploy-local
deploy-local:
	hack/deploy-local.sh

.PHONY: create-buildconfig
create-buildconfig:
	hack/create-buildconfig.sh

.PHONY: cluster-build
cluster-build:
	oc -n openshift-storage start-build lvms-operator --follow --wait

.PHONY: cluster-deploy
cluster-deploy:
	IMAGE_REGISTRY=image-registry.openshift-image-registry.svc:5000 \
	REGISTRY_NAMESPACE=openshift-storage \
	$(MAKE) deploy

# Security Analysis
SEVERITY_THRESHOLD ?= medium
SNYK_ORG ?= 81de31f3-6dff-46ff-af37-664e272a9fe3

.PHONY: vuln-scan-code
vuln-scan-code:
	snyk code test --project-name=lvms --severity-threshold=$(SEVERITY_THRESHOLD) --org=$(SNYK_ORG) --report

.PHONY: vuln-scan-deps
vuln-scan-deps:
	snyk test --project-name=lvms --severity-threshold=$(SEVERITY_THRESHOLD) --org=$(SNYK_ORG) --report

.PHONY: vuln-scan-container
vuln-scan-container:
	snyk container test $(IMAGE_REPO)/$(IMAGE_TAG) --severity-threshold=$(SEVERITY_THRESHOLD) --org=$(SNYK_ORG)
