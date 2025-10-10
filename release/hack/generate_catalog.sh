#!/bin/bash

opm_path="${1}"

template_file="release/catalog/lvm-operator-catalog-template.yaml"

echo "Generating catalog based on ${template_file}"
output_file="release/catalog/lvm-operator-catalog.json"

catalog=$(${opm_path} alpha render-template semver "--migrate-level=bundle-object-to-csv-metadata" -o yaml ${template_file})

channel="stable-${CATALOG_VERSION}"
latest_version=$(echo "${catalog}" | ch="${channel}" yq 'select(.schema == "olm.channel" and .name == strenv(ch)).entries[-1].name' | cut -d. -f2-)
latest_version=${latest_version:1} # Strip the leading v
skip_range=">=4.2.0 <${latest_version}"

# Set the skip range for the latest version of the operator
echo "Setting the skip range for 'v${latest_version}' on channel ${channel} to '${skip_range}'"
patched_catalog=$(echo "${catalog}" | ch="${channel}" sr="${skip_range}" yq -e e 'select(.schema == "olm.channel" and .name == strenv(ch)).entries[-1].skipRange = strenv(sr)')

# The semver render-template function by default names the channels "stable-vX.Y" but we historically don't use the "v" in our channel names
echo "Patching the channel names to use legacy naming conventions"
patched_catalog=$(echo "${patched_catalog}" | yq -o=json eval-all '(.. | select(tag == "!!str")) |= sub("stable-v", "stable-") | (.. | select(tag == "!!str")) |= sub("candidate-v", "candidate-")')

echo "${patched_catalog}" > ${output_file}

echo "Catalog generated: ${output_file}"
