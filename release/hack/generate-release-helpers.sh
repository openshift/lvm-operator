#!/bin/bash

function append_jiras {
    # $1 is the base release notes
    # $2 is the comma separated list of jiras
    rc=$(echo -e "${1}" | yq '.spec.data.releaseNotes.issues += []')

    IFS=', ' read -ra jira_keys <<< "${2}"
    for key in ${jira_keys[@]}; do
        yqformat=".spec.data.releaseNotes.issues += {\"id\": \"$key\", \"source\": \"issues.redhat.com\"}"
        rc=$(echo -e "$rc" | yq "$yqformat")
    done

    echo -e "$rc"
}

function append_cves {
    # $1 is the base release notes
    # $2 is the comma separated list of jiras
    rc=$(echo -e "${1}" | yq '.spec.data.releaseNotes.cves += []')

    IFS=', ' read -ra jira_keys <<< "${2}"
    for key in ${jira_keys[@]}; do
        yqformat=".spec.data.releaseNotes.cves += {\"key\": \"$key\", \"component\": \"lvm-operator\"}"
        rc=$(echo -e "$rc" | yq "$yqformat")
    done

    echo -e "$rc"
}

url_base="quay.io/redhat-user-workloads/logical-volume-manag-tenant"
components=("lvm-operator" "lvms-must-gather" "lvm-operator-bundle")

function get_candidate_snapshot {
    search_tag=$1
    url_base=$2
    components=$3

    declare -a manifests

    for component in ${components[@]}; do
        component_manifest=$(skopeo inspect --format "{{.Digest}}" "docker://${url_base}/${component}:${search_tag}")

        manifests+=("${component}@${component_manifest}")
    done

    # Filter down the snapshots until we find one that has all the required manifests
    bundle_snapshots=$(oc get snapshots -o yaml --sort-by='.metadata.creationTimestamp' | yq '.items[] | [{"name": .metadata.name, "images": [.spec.components[].containerImage], "conditions": .status.conditions}]' )
    for manifest in ${manifests[@]}; do
        bundle_snapshots=$(echo -e "${bundle_snapshots}" | yq "[.[] | select(.images | contains([\"${url_base}/${manifest}\"]))]")
    done

    snapshot_count=$(echo -e "${bundle_snapshots}" | yq ". | length")
    snapshot=""
    if [[ "1" != "${snapshot_count}" ]]; then
        echo "Unable to select a bundle. The following bundles match all manifests and are in a shippable state:"
        echo -e "${bundle_snapshots}" | yq ".[].name"

        echo -e "\nPlease select one of the candidate bundles and rerun with the RELEASE_SNAPSHOT environment variable"
        exit 1
    else
        echo $(echo -e "${bundle_snapshots}" | yq ".[0].name")
    fi
}
