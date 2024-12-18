#!/bin/bash -ex

echo "render_templates START..."

export OCS_VERSION=$(echo $CI_VERSION | cut -d'.' -f -2)

# Get an actual current release to update full_version properly
RELEASE=$(cat release/bundle/bundle.konflux.Dockerfile|egrep -e 'release="' | sed -e 's|^.*release="\(.*\)".*$|\1|')

# Set the version
export CSV_VERSION="${CI_VERSION}"

rm -rf bundle/manifests

### Define the environment
# Versions are tied across all the bundles DS
export OPERATOR_VERSION="$CSV_VERSION"

# Set the channel specs
export CHANNELS="stable-$OCS_VERSION"
export DEFAULT_CHANNEL="stable-$OCS_VERSION"

# Set skipRange and replaces for clean upgrade paths
export SKIP_RANGE=">=4.2.0 <${OPERATOR_VERSION}"
CSV_Z_VERSION=$(echo "${CSV_VERSION}" | cut -d '.' -f 3)
# Add the replaces line for non-X.Y.0 builds to the CSV to keep the older bundles unmasked
if test "$CSV_Z_VERSION" -gt 0; then
        export REPLACES="lvms-operator.v$OCS_VERSION.$(($CSV_Z_VERSION - 1))"
fi

make bundle-base

cat >> "bundle/manifests/lvms-operator.clusterserviceversion.yaml" << EOF
  relatedImages:
  - image: $IMG
    name: lvms-operator
  - image: $LVM_MUST_GATHER
    name: lvms-must-gather
EOF

# Add the full_version label for QE to be able to identify the build
sed -i "/^  labels:\$/a\    full_version: $CSV_VERSION-$RELEASE" bundle/manifests/lvms-operator.clusterserviceversion.yaml

# Update the example lvms-operator image
sed -i "s|quay.io/lvms_dev/lvms-operator:.*\$|$IMG|g" bundle/manifests/lvms-operator.clusterserviceversion.yaml
