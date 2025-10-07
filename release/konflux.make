SHELL := /bin/bash

AUTH_FILE?=$(shell echo ${XDG_RUNTIME_DIR}/containers/auth.json)
TARGET?="operator"
Y_STREAM?="v4.19"

.PHONY: rpm-lock
rpm-lock: rhsm-keys
	@echo $(AUTH_FILE)
	podman run --rm -it \
	-v $(shell pwd):/source:z \
	-v $(AUTH_FILE):/.dockerconfig.json:z \
	--env 'REGISTRY_AUTH_FILE=/.dockerconfig.json' \
	--env 'RHSM_ACTIVATION_KEY=$(RHSM_ACTIVATION_KEY)' \
	--env 'RHSM_ORG=$(RHSM_ORG)' \
	--workdir /source \
	registry.redhat.io/rhel9-4-els/rhel:9.4 ./release/hack/generate-rpm-lock.sh $(TARGET)

.PHONY: rhsm-keys
rhsm-keys:
ifndef RHSM_ACTIVATION_KEY
	$(error environment variable RHSM_ACTIVATION_KEY is required)
endif
ifndef RHSM_ORG
	$(error environment variable RHSM_ORG is required)
endif

.PHONY: konflux-update
konflux-update: konflux-task-manifest-updates

.PHONY: konflux-task-manifest-updates
konflux-task-manifest-updates:
	release/hack/update-konflux-task-refs.sh .tekton/single-arch-build-pipeline.yaml .tekton/multi-arch-build-pipeline.yaml .tekton/catalog-build-pipeline.yaml

.PHONY: catalog-template
catalog-template:
	TARGET_VERSIONS=$(Y_STREAM) release/hack/generate_catalog_template.sh
	@echo "Templates generation completed"
	@echo "To build the catalog file, run: make catalog-source"

.PHONY: catalog-source
catalog-source: opm
	CATALOG_VERSION=$(Y_STREAM) release/hack/generate_catalog.sh $(OPM)

IMAGE_BUILD_CMD ?= $(shell command -v podman 2>&1 >/dev/null && echo podman || echo docker)

.PHONY: catalog-container
catalog-container:
	$(IMAGE_BUILD_CMD) build --build-arg=CATALOG_VERSION=$(Y_STREAM) -t lvm-operator-catalog:$(Y_STREAM) -f release/catalog/catalog.konflux.Dockerfile .
