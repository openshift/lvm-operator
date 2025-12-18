#!/bin/bash
set -euo pipefail
: ${PULL_NUMBER:=""}
: ${REPO_OWNER:="openshift"}
: ${REPO_NAME:="lvm-operator"}
: ${KONFLUX_SA_TOKEN:=""}
: ${KONFLUX_SERVER:=""}
: ${DRYRUNONLY:=""}

commit_data=$(curl -s -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/pulls/${PULL_NUMBER}/files")
changes=$(echo "${commit_data}" | jq '{"changes": [.[] | select(.filename|test("releases/*")) | .filename]}')

echo "Changes detected:"
echo "${changes}" | jq ".changes[]"

KUBECONFIG="./konflux-config"

oc="oc --kubeconfig='${KUBECONFIG}'"

# Login to the Konflux Server
if ! $oc login --token="${KONFLUX_SA_TOKEN}" --server="${KONFLUX_SERVER}"; then
    status=$?
    echo "Error: Unable to login with the provided token. Exiting.."
    exit 1
fi

$oc project "logical-volume-manag-tenant"

echo "Do I have appropriate permissions to do a release?"
perms=$($oc auth can-i create releases)
if ! [ $? ] || [ "${perms}" == "no" ]; then
    echo "Error: The auth token does not have permissions to create a release."
    exit 1
fi

echo "Applying release configurations"

dry_run_flag=""
if ! [ -z "${DRYRUNONLY}" ]; then
    dry_run_flag="--dry-run=server"
fi

for change in $(echo "${changes}" | jq '.changes[]'); do
    echo "Applying ${change}"
    if ! $oc apply $dry_run_flag -f "${change}" -o yaml; then
        status=$?
        echo "Error: Unable to apply ${change}. Return code: ${status}"
        exit 1
    fi
done
