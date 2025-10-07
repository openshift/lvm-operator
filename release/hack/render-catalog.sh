#!/bin/bash

catalog_file="release/catalog/lvm-operator-catalog.json"

catalog_content=$(cat ${catalog_file})

# Update any pre-release references to be registry.redhat.io
catalog_content="${catalog_content//registry.stage/registry}"

echo "${catalog_content}" > $catalog_file
echo "Catalog patched at ${catalog_file}"
