#!/bin/bash

command -v yq >/dev/null 2>&1 || { echo >&2 "'yq' is required but it's not installed.  Aborting."; exit 1; }
command -v skopeo >/dev/null 2>&1 || { echo >&2 "'skopeo' is required but it's not installed.  Aborting."; exit 1; }

PIPELINE_FILE=""

function update_manifest_if_outdated() {
    image=$(echo $1 | cut -d '@' -f 1)
    manifest=$(echo $1 | cut -d '@' -f 2)

    new_manifest=$(skopeo inspect --format='{{ .Digest }}' "docker://${image}")
    if [[ $? -ne 0 ]]; then
        echo "error encountered running skopeo inspect against ${image}.  Aborting."; exit 1
    fi

    if [[ "$new_manifest" == "$manifest" ]]; then
        return # no new manifest
    fi

    if update_manifest $image $manifest $new_manifest; then
        echo "Updated manifest for ${image}:"
        echo "${manifest} => ${new_manifest}"

    else
        echo "unable to patch ${image}.  Aborting."; exit 1
    fi
}

function update_manifest() {
    image=$1
    old_manifest=$2
    new_manifest=$3

    ret=0
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' -e "s%${image}@${old_manifest}%${image}@${new_manifest}%g" $PIPELINE_FILE
    else
        sed -i -e "s%${image}@${old_manifest}%${image}@${new_manifest}%g" $PIPELINE_FILE
    fi
    return $?
}

for PIPELINE_FILE in "$@"; do
    echo "Checking ${PIPELINE_FILE} for task manifest updates..."

    active_manifests=()
    # Fetch the manifests that are currently used in our pipelines
    IFS=$'\n' read -r -d '' -a active_manifests < <( yq '.spec.tasks[].taskRef.params | filter(.name == "bundle") | .[].value' $PIPELINE_FILE && printf '\0' )

    for manifest in ${active_manifests[@]}; do
        update_manifest_if_outdated $manifest
    done
done
