CLUSTER_OS ?= rhel9
CANDIDATE_VERSION ?= 4.20
CATALOG_SOURCE ?= lvm-operator-catalogsource

.PHONY: image-digest-mirrors
image-digest-mirrors:
	./hack/generate_imagedigestmirrors.sh

.PHONY: update-cluster-pull-secret
update-cluster-pull-secret:
	./hack/update_cluster_pull_secret.sh

.PHONY: cluster-config
cluster-config: image-digest-mirrors update-cluster-pull-secret
	rm -rf manifests

.PHONY: cluster-catalog-config
cluster-catalog-config:
	TARGET_VERSION=$(CANDIDATE_VERSION) CATALOG_SOURCE=$(CATALOG_SOURCE) ./hack/configure_cluster_for_testing.sh

.PHONY: install-operator
install-operator:
	OPERATOR_CHANNEL="stable-$(CANDIDATE_VERSION)" CATALOG_SOURCE=$(CATALOG_SOURCE) ./hack/generate_operator_install_manifests.sh
