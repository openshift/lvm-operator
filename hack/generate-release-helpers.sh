#!/bin/bash

function append_jiras {
    # $1 is the base release notes
    # $2 is the comma separated list of jiras
    rc=$(echo -e "${1}" | yq '.spec.data.releaseNotes.issues.fixed += []')

    IFS=', ' read -ra jira_keys <<< "${2}"
    for key in ${jira_keys[@]}; do
        yqformat=".spec.data.releaseNotes.issues.fixed += {\"id\": \"$key\", \"source\": \"issues.redhat.com\"}"
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
