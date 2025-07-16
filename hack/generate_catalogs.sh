#!/bin/bash

opm_path="${1}"

files="templates/*-template.yaml"

for file in ${files}; do
    echo "Generating catalog based on ${file}"
    file_name=$(basename "${file}")
    output_file="catalogs/${file_name/-template\.yaml/}.json"

    version=""
    regex="(v[0-9]+\.[0-9]+)"
    if [[ $file_name =~ $regex ]]; then
        version="${BASH_REMATCH[1]}"
    fi

    # 4.17+ needs the --migrate-level=bundle-object-to-csv-metadata flag
    skip_flag_versions="v4.14 v4.15 v4.16"
    opm_migrate_level_flag="--migrate-level=bundle-object-to-csv-metadata"

    if [[ $skip_flag_versions =~ $version ]]; then
        echo "Skipping migrate-level flag for version ${version}"
        opm_migrate_level_flag=""
    fi

    catalog=$(${opm_path} alpha render-template semver ${opm_migrate_level_flag} -o yaml ${file})

    # The semver render-template function by default names the channels "stable-vX.Y" but we historically don't use the "v" in our channel names
    patched_catalog=$(echo "${catalog}" | yq -o=json eval-all '(.. | select(tag == "!!str")) |= sub("stable-v", "stable-") | (.. | select(tag == "!!str")) |= sub("candidate-v", "candidate-")')
    echo "${patched_catalog}" > ${output_file}

    echo "Catalog generated: ${output_file}"
done
