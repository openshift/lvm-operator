#!/bin/bash
source hack/generate-release-helpers.sh

: ${RELEASE_ENV:="production"} # Can be 'staging' or 'production'
: ${RELEASE_TYPE:="bundle"} # Can be 'bundle' or 'catalog'

: ${RELEASE_SNAPSHOT:=""}
if [[ -z "${RELEASE_SNAPSHOT}" ]]; then
    echo "No release snapshot specified. Please specify the snapshot to be released via the RELEASE_SNAPSHOT environment variable"
    exit 1
fi
echo "Using snapshot \"${RELEASE_SNAPSHOT}\""

if [[ $RELEASE_SNAPSHOT == "lvm-operator-catalog"* ]]; then
    RELEASE_TYPE="catalog"
fi

: ${AUTHOR:=""}
if [[ -z "${AUTHOR}" ]]; then
    echo "Setting author information from git configuration"
    AUTHOR="$(git config --get user.name) <$(git config --get user.email)>"
fi

if [[ "${RELEASE_TYPE}" == "bundle" ]]; then
    git_sha=$(oc -n logical-volume-manag-tenant get snapshot "${RELEASE_SNAPSHOT}" -o yaml | yq -e '(.metadata.annotations."build.appstudio.redhat.com/commit_sha")')
    bundle_build_url="https://raw.githubusercontent.com/openshift/lvm-operator/${git_sha}/release/container-build.args"

    echo "Downloading the container build args from ${bundle_build_url}"
    # Parse the args we use for building the bundle to pull version information
    while IFS='=' read -r arg value; do
        # Check if the line is not empty and contains an equals sign
        if [[ -n "$arg" && -n "$value" ]]; then
            # Assign the value to a variable with the argument name
            eval "$arg=\"$value\""
        fi
    done < <(curl "${bundle_build_url}")

    LVMS_TAGS="${LVMS_TAGS//v4./4-}"
fi

application="lvm-operator"
if [[ "${RELEASE_TYPE}" == "catalog" ]]; then
    application="lvm-operator-catalog"
    LVMS_TAGS="${RELEASE_SNAPSHOT:21:4}"
fi

release_plan="${application}-${RELEASE_ENV}-releaseplan-${LVMS_TAGS}"

release_name="lvm-operator-rhba-v${OPERATOR_VERSION}"
if [[ "${RELEASE_TYPE}" == "catalog" ]]; then
    release_name="${RELEASE_SNAPSHOT}"
fi

if [[ "${RELEASE_ENV}" == "staging" ]]; then
    release_name="${release_name}-staging-$(head /dev/urandom | LC_ALL=C tr -dc a-z0-9 | head -c8)"
fi

release_config_base=$(cat <<EOF
---
apiVersion: appstudio.redhat.com/v1alpha1
kind: Release
metadata:
    name: ${release_name}
    namespace: logical-volume-manag-tenant
    labels:
        release.appstudio.openshift.io/author: "${AUTHOR}"
spec:
    releasePlan: ${release_plan}
    snapshot: ${RELEASE_SNAPSHOT}
    data:
        releaseNotes:
            description: |
                After a successful LVMS upgrade, the latest lvms-operator SHA image should be seen.

                This erratum implements the following changes:

                - {!! TODO !!}

                Users of LVMS are advised to upgrade to the latest version in OpenShift Container Platform, which fixes these bugs and adds these enhancements.
EOF
)

release_config=""

# We don't need to populate data for catalogs
if [[ "${RELEASE_TYPE}" == "catalog" ]]; then
    release_config="$(echo "${release_config_base}" | yq 'del(.spec.data)')"
fi

# Do we have any JIRA issues or CVEs to add?
if [[ "${RELEASE_TYPE}" == "bundle" ]]; then
    jiras=""
    release_config="${release_config_base}"

    if [[ ! -z "${JIRA_XML}" ]]; then
        # Parse the Jira XML to pull out the Jira keys and summaries
        jiras=$(yq --xml-skip-proc-inst --xml-skip-directives -oy '[.rss.channel.item] | [{"key": .[].key.+content, "summary": .[].summary}]' ${JIRA_XML})

        keys=$(echo "${jiras}" | yq '[.[].key] | join(", ")')

        if [[ ! -z "${jiras}" ]]; then
            echo "Appending the following Jiras: ${keys}"
            release_config=$(append_jiras "${release_config}" "${keys}")

            cves=$(echo "${jiras}" | yq '.[].summary | match("CVE-\d{4}-\d{4,7}") | [.string] | join(", ")')
            if [[ ! -z "${cves}" ]]; then
                echo "Appending the following CVEs: ${cves}"
                release_config=$(append_cves "${release_config}" "${cves}")
            fi
        fi
    fi
fi

output_file="releases/${release_name}.yaml"
echo "Saving release config to ${output_file}"
echo -e "${release_config}" > $output_file
