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
: "${RELEASE_TIMEOUT_SECONDS:=3600}"
: "${RELEASE_POLL_SECONDS:=60}"

case "${JOB_TYPE}" in
    presubmit)
        url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/pulls/${PULL_NUMBER}/files"
        ;;
    postsubmit)
        url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/commits/${PULL_BASE_SHA}"
        ;;
    "")
        echo "No job type specified. Set JOB_TYPE to presubmit or postsubmit." >&2
        exit 1
        ;;
    *)
        echo "Unsupported job type '${JOB_TYPE}', skipping."
        exit 0
        ;;
esac

commit_data=$(curl --fail --silent --show-error --location \
    -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "${url}")
changes=$(echo "${commit_data}" | jq -r '
    (if type == "array" then . else .files end)
    | [.[] | select(.status != "removed")
        | .filename | select(test("^releases/.*\\.ya?ml$"))] | .[]')

if [[ -z "${changes}" ]]; then
    echo "No release object changes detected, skipping."
    exit 0
fi

if [[ ! "${RELEASE_TIMEOUT_SECONDS}" =~ ^[1-9][0-9]*$ || ! "${RELEASE_POLL_SECONDS}" =~ ^[1-9][0-9]*$ ]]; then
    echo "RELEASE_TIMEOUT_SECONDS and RELEASE_POLL_SECONDS must be positive integers." >&2
    exit 1
fi

oc=(oc --kubeconfig ./konflux-config --namespace "${KONFLUX_PROJECT}")
"${oc[@]}" login --token="${KONFLUX_SA_TOKEN}" --server="${KONFLUX_SERVER}" > /dev/null

can_create=$("${oc[@]}" auth can-i create releases)
if [[ "${can_create}" != "yes" ]]; then
    echo "The auth token does not have permission to create releases." >&2
    exit 1
fi

bundle_files=()
catalog_files=()
declare -A release_names

# Check the whole batch before creating anything. A later name clash must not
# leave a bundle submitted while its catalog cannot be created.
while IFS= read -r change; do
    manifest=$("${oc[@]}" create --dry-run=client -f "${change}" -o json | jq -e --arg namespace "${KONFLUX_PROJECT}" '
        if .kind == "Release"
            and (.metadata.name | type == "string" and length > 0)
            and ((.metadata.namespace // $namespace) == $namespace)
            and (.spec.releasePlan | type == "string" and length > 0)
        then . else error("Expected one named Release in the target namespace") end')
    name=$(echo "${manifest}" | jq -r '.metadata.name')
    plan=$(echo "${manifest}" | jq -r '.spec.releasePlan')

    if [[ -n "${release_names[${name}]:-}" ]]; then
        echo "Release name '${name}' appears in more than one changed file." >&2
        exit 1
    fi

    existing=$("${oc[@]}" get release "${name}" --ignore-not-found -o name)
    if [[ -n "${existing}" ]]; then
        echo "Release '${name}' already exists. Use a new release name." >&2
        exit 1
    fi

    release_names[${name}]=1
    case "${plan}" in
        lvm-operator-catalog-*) catalog_files+=("${change}") ;;
        lvm-operator-*) bundle_files+=("${change}") ;;
        *)
            echo "Unknown release plan '${plan}' in ${change}." >&2
            exit 1
            ;;
    esac
done <<< "${changes}"

wait_for_release() {
    local name=$1
    local deadline=$((SECONDS + RELEASE_TIMEOUT_SECONDS))
    local release condition status reason remaining delay

    echo "Waiting for bundle release '${name}' to succeed."
    while true; do
        release=$("${oc[@]}" get release "${name}" -o json)
        condition=$(echo "${release}" | jq -c '[.status.conditions[]? | select(.type == "Released")][0] // {}')
        status=$(echo "${condition}" | jq -r '.status // "Unknown"')
        reason=$(echo "${condition}" | jq -r '.reason // ""')

        if [[ "${status}" == "True" ]]; then
            echo "Bundle release '${name}' succeeded."
            return 0
        fi

        if [[ "${status}" == "False" && "${reason}" != "Progressing" && -n "${reason}" ]]; then
            echo "Bundle release '${name}' failed: ${condition}" >&2
            return 1
        fi

        remaining=$((deadline - SECONDS))
        if (( remaining <= 0 )); then
            echo "Timed out waiting for bundle release '${name}': ${condition}" >&2
            return 1
        fi

        delay=${RELEASE_POLL_SECONDS}
        if (( delay > remaining )); then
            delay=${remaining}
        fi

        sleep "${delay}"
    done
}

create_flags=()
if [[ -n "${DRYRUNONLY}" ]]; then
    echo "Enabling server dry run; release completion will not be checked."
    create_flags+=(--dry-run=server)
fi

for change in "${bundle_files[@]}"; do
    echo "Creating bundle release from ${change}."
    created=$("${oc[@]}" create "${create_flags[@]}" -f "${change}" -o json)
    echo "${created}"

    if [[ -z "${DRYRUNONLY}" ]]; then
        name=$(echo "${created}" | jq -er '.metadata.name')
        wait_for_release "${name}"
    fi
done

# Catalog-only changes remain supported. When bundles are in this change,
# every bundle above must have succeeded before any catalog is submitted.
for change in "${catalog_files[@]}"; do
    echo "Creating catalog release from ${change}."
    "${oc[@]}" create "${create_flags[@]}" -f "${change}" -o yaml
done
