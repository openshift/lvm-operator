#!/bin/bash

catalog_file="release/catalog/lvm-operator-catalog.json"

# OCPBUGS-76865: Override the default channel to force 4.17 instead of 4.18
echo "Setting default channel to stable-4.17"
yq eval-all --inplace -o=json 'select(document_index == 0).defaultChannel = "stable-4.17"' ${catalog_file}

catalog_content=$(cat ${catalog_file})

# Channel name patching to match legacy format (no v)
echo "Patching channel names to match legacy format"
catalog_content="${catalog_content//stable-v/stable-}"

# Set the skip range for the latest version of the operator
channel=$(echo "${catalog_content}" | yq -p=json 'select(.schema == "olm.package").defaultChannel')
latest_version=$(echo "${catalog_content}" | ch="${channel}" yq -p=json 'select(.schema == "olm.channel" and .name == strenv(ch)).entries[-1].name' | cut -d. -f2-)
latest_version=${latest_version:1} # Strip the leading v
skip_range=">=4.2.0 <${latest_version}"

echo "Setting the skip range for 'v${latest_version}' on channel ${channel} to '${skip_range}'"
catalog_content=$(echo "${catalog_content}" | ch="${channel}" sr="${skip_range}" yq -p=json -o=json -e e 'select(.schema == "olm.channel" and .name == strenv(ch)).entries[-1].skipRange = strenv(sr)')

echo "${catalog_content}" > $catalog_file
echo "Catalog patched at ${catalog_file}"
