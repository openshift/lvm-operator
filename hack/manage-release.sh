#!/bin/bash
set -euo pipefail
: "${PULL_NUMBER:=""}"
: "${PULL_BASE_SHA:=""}"
: "${REPO_OWNER:="openshift"}"
: "${REPO_NAME:="lvm-operator"}"
: "${KONFLUX_SA_TOKEN:=""}"
: "${KONFLUX_SERVER:="https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443"}"
: "${KONFLUX_PROJECT:="logical-volume-manag-tenant"}"
: "${DRYRUNONLY:=""}"
: "${JOB_TYPE:=""}"

changes=""

if [ "${JOB_TYPE}" == "presubmit" ]; then
    commit_data=$(curl -s -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/pulls/${PULL_NUMBER}/files")
    changes=$(echo "${commit_data}" | jq '{"changes": [.[] | select(.filename|test("releases/*")) | .filename]}')
elif [ "${JOB_TYPE}" == "postsubmit" ]; then
    commit_data=$(curl -s -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/commits/${PULL_BASE_SHA}")
    changes=$(echo "${commit_data}" | jq '{"changes": [.files[] | select(.filename|test("releases/*")) | .filename]}')
elif [ -z "${JOB_TYPE}" ]; then
    echo "No job type specified, please specify either 'presubmit' or 'postsubmit' via the JOB_TYPE env"
    exit 1
else
    echo "Unsupported job type '${JOB_TYPE}' specified. Only presubmit and postsubmit jobs are supported. Skipping.."
    exit 0
fi

if echo "${changes}" | jq -e '.changes | length' > /dev/null; then
    echo "Release object changes detected:"
    echo "${changes}" | jq ".changes[]"
    echo ""
else
    echo "No release object changes detected, skipping.."
    exit 0
fi


KUBECONFIG="./konflux-config"

oc="oc --kubeconfig ${KUBECONFIG}"

# Login to the Konflux Server
if ! $oc login --token="${KONFLUX_SA_TOKEN}" --server="${KONFLUX_SERVER}" > /dev/null; then
    status=$?
    echo "Error: Unable to login with the provided token. Exiting.."
    exit 1
fi

$oc project "${KONFLUX_PROJECT}"

perms=$($oc auth can-i create releases)
status=$?
echo -e "\nDo I have appropriate permissions to do a release? \033[1m${perms}\033[0m\n"

if ! [ $status ] || [ "${perms}" == "no" ]; then
    echo "Error: The auth token does not have permissions to create a release."
    exit 1
fi

dry_run_flag=""
if ! [ -z "${DRYRUNONLY}" ]; then
    echo -e "\u2705 Enabling dry run mode"
    dry_run_flag="--dry-run=server"
fi

for change in $(echo "${changes}" | jq '.changes[]'); do
    echo -e "\n\033[1mApplying ${change}\033[0m\n---"
    if ! $oc apply $dry_run_flag -f "${change}" -o yaml; then
        status=$?
        echo "Error: Unable to apply ${change}. Return code: ${status}"
        exit 1
    fi
done
