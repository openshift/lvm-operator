.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.51.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

all: templates catalogs

.PHONY: konflux-update
konflux-update: konflux-task-manifest-updates

.PHONY: konflux-task-manifest-updates
konflux-task-manifest-updates:
	hack/update-konflux-task-refs.sh .tekton/catalog-pipeline.yaml

.PHONY: templates
templates:
	./hack/generate_released_templates.sh
	@echo "Templates generated in templates/"
	@echo "To build the catalog files, run: make catalogs"
	@echo "To build a single catalog file, run: CATALOG_VERSION={{ catalog_version }} make catalog"

# Catalog generation
CATALOG_VERSION ?= v4.12

OPM_NO_FLAG_VERSIONS = v4.12 v4.13 v4.14 v4.15 v4.16
OPM_CMD = $(OPM) alpha render-template semver --migrate-level=bundle-object-to-csv-metadata
ifneq ($(filter $(CATALOG_VERSION),$(OPM_NO_FLAG_VERSIONS)),) # Pre-4.17
OPM_CMD = $(OPM) alpha render-template semver
endif
.PHONY: catalog
catalog: opm
	$(OPM_CMD) -o json templates/lvm-operator-catalog-$(CATALOG_VERSION)-template.yaml > catalogs/lvm-operator-catalog-$(CATALOG_VERSION).json
	@echo "Catalog $(CATALOG_VERSION) generated in catalogs/lvm-operator-catalog-$(CATALOG_VERSION).json"
	@echo "To build the catalog image, run: CATALOG_VERSION=$(CATALOG_VERSION) make container"

.PHONY: catalogs
catalogs: opm
	./hack/generate_catalogs.sh $(OPM)

# Catalog Container Builds
IMAGE_BUILD_CMD ?= $(shell command -v podman 2>&1 >/dev/null && echo podman || echo docker)
RHEL8_VERSIONS = v4.12 v4.13 v4.14
CATALOG_BUILD_ARGS = --build-arg=CATALOG_VERSION="$(CATALOG_VERSION)"
ifneq ($(filter $(CATALOG_VERSION),$(RHEL8_VERSIONS)),) # Pre-4.15
CATALOG_BUILD_ARGS += --build-arg=BASE_IMAGE="registry.redhat.io/openshift4/ose-operator-registry"
endif

.PHONY: container
container:
	$(IMAGE_BUILD_CMD) build $(CATALOG_BUILD_ARGS) -t lvm-operator-catalog:$(CATALOG_VERSION) -f konflux-catalog.Dockerfile

# Testing Workflow Commands
CATALOG_SOURCE_IMAGE ?= quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-catalog
CATALOG_SOURCE_IMAGE_TAG ?= $(CATALOG_VERSION)

.PHONY: catalog-source
catalog-source:
	./hack/generate_catalogsource.sh

CLUSTER_OS ?= rhel9

.PHONY: image-digest-mirrors
image-digest-mirrors:
	./hack/generate_imagedigestmirrors.sh

.PHONY: update-cluster-pull-secret
update-cluster-pull-secret:
	./hack/update_cluster_pull_secret.sh

.PHONY: operator-install
install-manifests:
	./hack/generate_operator_install_manifests.sh

.PHONY: test-manifests
test-manifests:
	rm -rf manifests
	mkdir -p manifests
	$(MAKE) catalog-source image-digest-mirrors install-manifests
	@echo "Manifests generated in manifests/"
	@echo "To update the cluster pull secret, run: make update-cluster-pull-secret"
	@echo "To apply the manifests, run: oc apply -R -f manifests"
