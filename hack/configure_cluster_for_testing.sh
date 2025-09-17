#!/bin/bash

: ${TARGET_VERSION:="4.20"}
: ${TEST_CLUSTER_KUBECONFIG:="${HOME}/.kube/config"}
: ${TEST_CLUSTER_CONTEXT:="admin"}
: ${TEST_SNAPSHOT:=""}
: ${CATALOG_SNAPSHOT:=""}
: ${CATALOG_SOURCE:="lvm-operator-catalogsource"}
version=$(echo "${TARGET_VERSION}" | tr . -)

konflux_namespace="logical-volume-manag-tenant"
konflux_cluster="https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443/"

if ! [ -n "${TEST_SNAPSHOT}" ]; then
    echo "ERROR: An operator snapshot must be supplied by setting the TEST_SNAPSHOT environment variable. Exiting..."
    exit 1
fi

if ! [ -n "${CATALOG_SNAPSHOT}" ]; then
    echo "ERROR: A catalog snapshot must be supplied by setting the CATALOG_SNAPSHOT environment variable. Exiting..."
    exit 1
fi

oc login --web $konflux_cluster

bundle_image=$(oc -n "${konflux_namespace}" get snapshot "${TEST_SNAPSHOT}" -o yaml | yq '.spec.components[] | select(.name == "lvm-operator-bundle-*") | .containerImage')
catalog_image=$(oc -n "${konflux_namespace}" get snapshot "${CATALOG_SNAPSHOT}" -o yaml | yq ".spec.components[] | select(.name == \"lvm-operator-catalog-${version}\") | .containerImage")

oc -n "${konflux_namespace}" get snapshot "${CATALOG_SNAPSHOT}" -o yaml | yq ".spec.components[] | select(.name == \"lvm-operator-catalog-${version}\") | .containerImage"

echo "Operator Snapshot: ${TEST_SNAPSHOT}"
echo "Operator Bundle: ${bundle_image}"
echo "Catalog Snapshot: ${CATALOG_SNAPSHOT}"
echo "Catalog Image: ${catalog_image}"

# Validate we got the correct information before doing configuration
read -p "Is this correct? (y/n): " -n 1 -r
echo
if ! [[ $REPLY =~ ^[Yy]$ ]]
then
    echo "Aborting cluster configuration..."
    exit 1
fi

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

# Setup configuration for the test cluster
unset KUBECONFIG
export KUBECONFIG="${TEST_CLUSTER_KUBECONFIG}"
oc config use-context "${TEST_CLUSTER_CONTEXT}"

# Disable default catalog sources
oc patch OperatorHub cluster --type json -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'

echo "${catalog_source}" | oc -n openshift-marketplace apply -f -

# Wait for the packagemanifest to be ready
while [ true ]; do
    packagemanifest_ready=$(oc -n openshift-marketplace get packagemanifest | grep "lvms-operator")
    if ! [ -n "${packagemanifest_ready}" ]; then
        echo "packagemanifest not yet ready, trying again in a few seconds..."
        sleep 5
        continue
    fi

    break
done

echo "Validating the installed catalog source contains the bundle under test"
bundle_sha=${bundle_image##*@}
staging_bundle_image="registry.redhat.io/lvms4/lvms-operator-bundle@${bundle_sha}"
bundle_channel=$(oc -n openshift-marketplace get packagemanifest lvms-operator -o yaml | yq ".status.channels[] | select(.currentCSVDesc.relatedImages[] == \"${staging_bundle_image}\") | .name")

if [ -n "${bundle_channel}" ]; then
    echo "Bundle with manifest ${bundle_sha} successfully identified in channel ${bundle_channel}"
else
    echo "ERROR: could not find the bundle with manifest ${bundle_sha} in the sourced catalog. Exiting..."
    exit 1
fi
