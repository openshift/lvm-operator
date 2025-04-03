all: templates catalogs

.PHONY: templates
templates:
	./hack/generate_released_templates.sh

CATALOG_VERSION ?= v4.12
catalog: opm
	$(OPM) alpha render-template semver -o yaml templates/lvm-operator-catalog-$(CATALOG_VERSION)-template.yaml > catalogs/lvm-operator-catalog-$(CATALOG_VERSION).yaml

catalogs: opm
	./hack/generate_catalogs.sh $(OPM)

IMAGE_BUILD_CMD ?= $(shell command -v docker 2>&1 >/dev/null && echo docker || echo podman)
.PHONY: container
container:
	podman build --build-arg=CATALOG_VERSION="$(CATALOG_VERSION)" -t lvm-operator-catalog:$(CATALOG_VERSION) -f konflux-catalog.Dockerfile

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.48.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif
