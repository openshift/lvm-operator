#!/bin/bash
source release/hack/generate-release-helpers.sh

# Parse the args we use for building the bundle to pull version information
while IFS='=' read -r arg value; do
    # Check if the line is not empty and contains an equals sign
    if [[ -n "$arg" && -n "$value" ]]; then
        # Assign the value to a variable with the argument name
        eval "$arg=\"$value\""
    fi
done < "release/container-build.args"

: ${RELEASE_ENV:="staging"}

source_base_url="quay.io/redhat-user-workloads/logical-volume-manag-tenant"
components=("lvm-operator" "lvms-must-gather" "lvm-operator-bundle")
release_plan="lvm-operator-stage-releaseplan"
if [[ "${RELEASE_ENV}" == "production" ]]; then
    source_base_url="registry.stage.redhat.io/lvms4"
    components=("lvms-rhel9-operator" "lvms-must-gather-rhel9" "lvms-operator-bundle")
    release_plan="lvm-operator-${LVMS_TAGS}-production-releaseplan"
fi

: ${RELEASE_SNAPSHOT:=""}
if [[ -z "${RELEASE_SNAPSHOT}" ]]; then
    echo "No release snapshot specified. Calculating target snapshot from v${OPERATOR_VERSION} on ${RELEASE_ENV}..."
    RELEASE_SNAPSHOT=$(get_candidate_snapshot "v${OPERATOR_VERSION}" $source_base_url $components)

    ret=$?
    if [ $ret -ne 0 ]; then
        echo -e "error: unable to get candidate release snapshot\nexiting..."
        exit $ret
    fi
fi
echo "Using snapshot \"${RELEASE_SNAPSHOT}\""

release_name="lvms-rhba-v${OPERATOR_VERSION}-$(head /dev/urandom | LC_ALL=C tr -dc a-z0-9 | head -c8)"

release_config_base=$(cat <<EOF
---
apiVersion: appstudio.redhat.com/v1alpha1
kind: Release
metadata:
    name: ${release_name}
    namespace: logical-volume-manag-tenant
    labels:
        release.appstudio.openshift.io/author: '${MAINTAINER}'
spec:
    releasePlan: ${release_plan}
    snapshot: ${RELEASE_SNAPSHOT}
EOF
)

jiras=""
release_config="${release_config_base}"
# Do we have any JIRA issues or CVEs to add?
if [[ ! -z "${JIRA_XML}" ]]; then
    # Parse the Jira XML to pull out the Jira keys and summaries
    jiras=$(yq --xml-skip-proc-inst --xml-skip-directives -oy '.rss.channel.item[] | [{"key": .key.+content, "summary": .summary}]' ${JIRA_XML})

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

# Append the tags
release_config=$(echo -e "${release_config}" | yq ".spec.data.mapping.defaults.tags += [\"v${OPERATOR_VERSION}\", \"${LVMS_TAGS}\", \"${LVMS_TAGS}-{{ timestamp }}\"] | .spec.mapping.defaults.tags[] style=\"double\"")

output_file="release/${release_name}.release.yaml"
echo "Saving release config to ${output_file}"
echo -e "${release_config}" > $output_file
