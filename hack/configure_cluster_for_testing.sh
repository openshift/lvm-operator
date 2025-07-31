#!/bin/bash

: ${TARGET_VERSION:="4.20"}
: ${TEST_CLUSTER_KUBECONFIG:="${HOME}/.kube/config"}
: ${TEST_CLUSTER_CONTEXT:="admin"}
: ${KONFLUX_CLUSTER_KUBECONFIG:="${HOME}/.kube/konflux.kubeconfig"}
: ${KONFLUX_CLUSTER_CONTEXT:="konflux"}
version=$(echo "${TARGET_VERSION}" | tr . -)

konflux_namespace="logical-volume-manag-tenant"
unset KUBECONFIG
export KUBECONFIG="${KONFLUX_CLUSTER_KUBECONFIG}"
oc config use-context "${KONFLUX_CLUSTER_CONTEXT}"

snapshot_name=$(oc -n "${konflux_namespace}" get releases -o custom-columns=SNAPSHOT:.spec.snapshot --sort-by=.metadata.creationTimestamp | grep "lvm-operator-${TARGET_VERSION}" | tail -n 1)
bundle_image=$(oc -n "${konflux_namespace}" get snapshot "${snapshot_name}" -o yaml | yq '.spec.components[] | select(.name == "lvm-operator-bundle-*") | .containerImage')

echo "Target Snapshot: ${snapshot_name}"
echo "Target Bundle: ${bundle_image}"

# Validate we got the correct information before doing configuration
read -p "Is this correct? (y/n): " -n 1 -r
echo
if ! [[ $REPLY =~ ^[Yy]$ ]]
then
    echo "Aborting cluster configuration..."
    exit 1
fi

bundle_sha=${bundle_image##*@}

# Get the latest release for the requested version
catalog_release=$(oc -n "${konflux_namespace}" get releases -o custom-columns=RELEASE:.metadata.name --sort-by=.metadata.creationTimestamp | grep "lvm-operator-catalog-${version}" | tail -n 1)
catalog_snapshot=$(oc -n "${konflux_namespace}" get release "${catalog_release}" -o yaml | yq '.spec.snapshot')
catalog_image=$(oc -n "${konflux_namespace}" get snapshot "${catalog_snapshot}" -o yaml | yq ".spec.components[] | select(.name == \"lvm-operator-catalog-${version}\") | .containerImage")

echo "Catalog Image: ${catalog_image}"

catalog_source=$(cat << EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${CATALOG_SOURCE}
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${catalog_image}
  displayName: LVM Operator CatalogSource
  publisher: Red Hat
EOF
)

export KUBECONFIG="${TEST_CLUSTER_KUBECONFIG}"
oc config use-context "${TEST_CLUSTER_CONTEXT}"

# Disable default catalog sources
oc patch OperatorHub cluster --type json -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'

echo "${catalog_source}" | oc -n openshift-marketplace apply -f -
