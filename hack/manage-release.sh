#!/bin/bash
: ${PULL_BASE_SHA:=""}
: ${REPO_OWNER:="openshift"}
: ${REPO_NAME:="lvm-operator"}
: ${KONFLUX_SA_TOKEN:=""}
: ${KONFLUX_SERVER:=""}
: ${KONFLUX_PROJECT:=""}
: ${DRYRUNONLY:=""}

commit_data=$(curl -s -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/commits/${PULL_BASE_SHA}")
changes=$(echo "${commit_data}" | jq '.files[] | select(.status == "added") | select(.filename|test("releases/*")) | {"changes": [.filename]}')

# Login to the Konflux Server
oc login --token=${KONFLUX_SA_TOKEN} --server=${KONFLUX_SERVER}

echo "Do I have appropriate permissions to do a release?"
oc auth can-i create releases --namespace=${KONFLUX_PROJECT}

echo "Applying release configurations to the ${KONFLUX_PROJECT} project on the ${KONFLUX_SERVER} server."

dry_run_flag=""
if ! [ -z "${DRYRUNONLY}" ]; then
    dry_run_flag="--dry-run=server"
fi

for change in $(echo "${changes}" | jq '.changes[]'); do
    echo "Applying ${change}"
    oc --namespace=${KONFLUX_PROJECT} apply $dry_run_flag -f $change
done
