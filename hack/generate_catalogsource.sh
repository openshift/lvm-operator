#!/bin/bash
set -euo pipefail

image="${CATALOG_SOURCE_IMAGE:-quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-catalog}"
image_tag="${CATALOG_SOURCE_IMAGE_TAG:-v4.19}"

mkdir -p manifests

cat <<EOF > manifests/catalogsource.yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: lvm-operator-catalogsource
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${image}:${image_tag}
  displayName: LVM Operator CatalogSource
  publisher: Red Hat
EOF

echo "CatalogSource manifest created at manifests/catalogsource.yaml"
