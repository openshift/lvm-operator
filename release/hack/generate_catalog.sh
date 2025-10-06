#!/bin/bash

opm_path="${1}"

template_file="release/catalog/lvm-operator-catalog-template.yaml"

echo "Generating catalog based on ${template_file}"
output_file="release/catalog/lvm-operator-catalog.json"

catalog=$(${opm_path} alpha render-template semver "--migrate-level=bundle-object-to-csv-metadata" -o yaml ${template_file})

# The semver render-template function by default names the channels "stable-vX.Y" but we historically don't use the "v" in our channel names
patched_catalog=$(echo "${catalog}" | yq -o=json eval-all '(.. | select(tag == "!!str")) |= sub("stable-v", "stable-") | (.. | select(tag == "!!str")) |= sub("candidate-v", "candidate-")')

echo "${patched_catalog}" > ${output_file}

echo "Catalog generated: ${output_file}"
