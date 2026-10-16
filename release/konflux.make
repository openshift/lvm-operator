SHELL := /bin/bash

export RHSM_ORG RHSM_ACTIVATION_KEY

AUTH_FILE?=$(shell echo ${XDG_RUNTIME_DIR}/containers/auth.json)
TARGET?="operator"
Y_STREAM?="v4.13"

.PHONY: rpm-lock
rpm-lock: rhsm-keys
	@echo $(AUTH_FILE)
	podman run --rm -it --user 0:0 \
	-v $(shell pwd):/source:z \
	-v $(AUTH_FILE):/.dockerconfig.json:z \
	--env 'REGISTRY_AUTH_FILE=/.dockerconfig.json' \
	--env RHSM_ACTIVATION_KEY \
	--env RHSM_ORG \
	--env 'SMDEV_CONTAINER_OFF=1' \
	--workdir /source \
	registry.redhat.io/ubi9/ubi:latest ./release/hack/generate-rpm-lock.sh $(TARGET)

.PHONY: rhsm-keys
rhsm-keys:
ifndef RHSM_ACTIVATION_KEY
	$(error environment variable RHSM_ACTIVATION_KEY is required)
endif
ifndef RHSM_ORG
	$(error environment variable RHSM_ORG is required)
endif

.PHONY: catalog-template
catalog-template:
	TARGET_VERSIONS=$(Y_STREAM) release/hack/generate_catalog_template.sh
	@echo "Templates generation completed"
	@echo "To build the catalog file, run: make catalog-source"

.PHONY: catalog-source
catalog-source: opm
	$(OPM) alpha render-template semver release/catalog/lvm-operator-catalog-template.yaml > release/catalog/lvm-operator-catalog.json

IMAGE_BUILD_CMD ?= $(shell command -v podman 2>&1 >/dev/null && echo podman || echo docker)

.PHONY: catalog-container
catalog-container:
	$(IMAGE_BUILD_CMD) build --build-arg=CATALOG_VERSION=$(Y_STREAM) -t lvm-operator-catalog:$(Y_STREAM) -f release/catalog/catalog.konflux.Dockerfile .
