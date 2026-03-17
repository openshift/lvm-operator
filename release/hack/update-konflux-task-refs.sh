#!/bin/bash
set -x

command -v yq >/dev/null 2>&1 || { echo >&2 "'yq' is required but it's not installed. Aborting."; exit 1; }
command -v skopeo >/dev/null 2>&1 || { echo >&2 "'skopeo' is required but it's not installed. Aborting."; exit 1; }
command -v pmt >/dev/null 2>&1 || { echo >&2 "'pmt' is required but it's not installed. Aborting."; exit 1; }

NEW_BUNDLES=()

# Collect all bundle references from all pipeline files
for PIPELINE_FILE in "$@"; do
    echo "Checking ${PIPELINE_FILE} for task manifest updates..."

    IFS=$'\n' read -r -d '' -a active_manifests < <( yq '.spec.tasks[].taskRef.params | filter(.name == "bundle") | .[].value' "$PIPELINE_FILE" && printf '\0' )

    for manifest in "${active_manifests[@]}"; do
        image=$(echo "$manifest" | cut -d '@' -f 1)
        current_digest=$(echo "$manifest" | cut -d '@' -f 2)

        if ! new_digest=$(skopeo inspect --format='{{ .Digest }}' "docker://${image}"); then
            echo "error encountered running skopeo inspect against ${image}. Aborting."; exit 1
        fi

        if [[ "$new_digest" != "$current_digest" ]]; then
            echo "Found update for ${image}:"
            echo "${current_digest} => ${new_digest}"
            NEW_BUNDLES+=("--new-bundle" "${image}@${new_digest}")
        fi
    done
done

# Apply migrations if there are any updates
if [[ ${#NEW_BUNDLES[@]} -gt 0 ]]; then
    echo "Applying migrations with pmt..."
    pmt migrate "${NEW_BUNDLES[@]}"
else
    echo "No updates found."
fi
