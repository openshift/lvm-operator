#!/bin/bash

opm_path="${1}"

files="templates/*-template.yaml"

for file in ${files}; do
    echo "Generating catalog based on ${file}"
    file_name=$(basename "${file}")
    output_file="catalogs/${file_name/-template/}"
    ${opm_path} alpha render-template semver -o yaml ${file} > ${output_file}
    echo "Catalog generated: ${output_file}"
done
