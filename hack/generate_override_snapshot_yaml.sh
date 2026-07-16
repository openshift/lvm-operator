#!/bin/bash
#
# get_related_images.sh
#
# Extract the related images from an operator bundle image's
# ClusterServiceVersion manifest.
#
# Usage: get_related_images.sh BUNDLE_IMAGE_REF
#
# Example:
#   get_related_images.sh quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle@sha256:abc123
#

set -euo pipefail

readonly TEMP_DIR="/tmp/get-related-images-$$"

usage() {
    cat <<EOF
Usage: $(basename "$0") BUNDLE_IMAGE_REF

Extract the related images from an operator bundle image.

Positional Arguments:
    BUNDLE_IMAGE_REF   Full bundle image reference (e.g., quay.io/org/bundle@sha256:...)

EOF
}

IMAGE_BASE_URL="quay.io/redhat-user-workloads/logical-volume-manag-tenant"

error() {
    echo "ERROR: $*" >&2
}

cleanup() {
    rm -rf "$TEMP_DIR"
}

trap cleanup EXIT

check_dependencies() {
    local missing=()

    for tool in skopeo jq yq tar; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            missing+=("$tool")
        fi
    done

    if [ ${#missing[@]} -gt 0 ]; then
        error "Missing required tools: ${missing[*]}"
        exit 1
    fi
}

get_image_sha(){
	local image_ref="${1}"
	image_ref=${image_ref#*@} # Drop anything before the 'sha:'
	echo ${image_ref%\"} # Remove the trailing quote
}

get_image_git_ref(){
	local image_ref="${1}"
	echo $(skopeo inspect --config docker://${image_ref} | jq -r '.config.Labels."vcs-ref"')
}

# Copies the bundle image locally and extracts its layers, leaving the
# ClusterServiceVersion manifest accessible under $TEMP_DIR/extracted.
extract_bundle_manifests() {
    local bundle_image="$1"
    local copy_dir="$TEMP_DIR/image"
    local extract_dir="$TEMP_DIR/extracted"

    mkdir -p "$copy_dir" "$extract_dir"

    if ! skopeo copy "docker://${bundle_image}" "dir:${copy_dir}" >/dev/null; then
        error "Failed to copy bundle image: $bundle_image"
        exit 1
    fi

    local layer_digest
    while IFS= read -r layer_digest; do
        layer_digest="${layer_digest#sha256:}"
        tar -xf "${copy_dir}/${layer_digest}" -C "$extract_dir"
    done < <(jq -r '.layers[].digest' "${copy_dir}/manifest.json")
}

find_csv_file() {
    find "$TEMP_DIR/extracted" -type f -iname '*.clusterserviceversion.yaml' | head -n1
}

# Parses the flat "- image: ...\n  name: ..." relatedImages list out of the
# ClusterServiceVersion yaml and emits it as a JSON object mapping each
# related image's name to its image reference.
extract_related_images() {
    local csv_file="$1"
    local related_images_block
    related_images_block=$(awk '/^  relatedImages:/{flag=1; next} flag && /^  [a-zA-Z]/{exit} flag' "$csv_file")

    if [ -z "$related_images_block" ]; then
        echo "{}"
        return
    fi

    local images names
    images=$(echo "$related_images_block" | grep -E '^\s*-\s*image:' | sed -E 's/^\s*-\s*image:\s*//')
    names=$(echo "$related_images_block" | grep -E '^\s*name:' | sed -E 's/^\s*name:\s*//')

    paste -d'\t' <(echo "$names") <(echo "$images") | jq -R -s '
    split("\n") | map(select(length > 0)) | map(split("\t")) | map({key: .[0], value: .[1]}) | from_entries
    '
}

main() {
    if [ $# -ne 1 ]; then
        usage
        exit 1
    fi

    local bundle_image="$1"

    check_dependencies

	# Get the needed data for the bundle
    VERSION=$(skopeo inspect --config docker://${bundle_image} | jq -r '.config.Labels.version')
    X="${VERSION%%.*}"
    Y="${VERSION#*.}"
    Y="${Y%.*}"
    Z="${VERSION##*.}"

    BUNDLE_GIT_REF=$(get_image_git_ref ${bundle_image})

	# Extract the manifests so we can get the needed info for the related images
    extract_bundle_manifests "$bundle_image"

    local csv_file
    csv_file=$(find_csv_file)
    if [ -z "$csv_file" ]; then
        error "No ClusterServiceVersion manifest found in bundle image: $bundle_image"
        exit 1
    fi

    local related_images
    related_images=$(extract_related_images "$csv_file")

	# Get the related image refs
	OPERAND_SHA=$(get_image_sha $(echo "${related_images}" | jq -r '."lvms-operator"'))
	MUST_GATHER_SHA=$(get_image_sha $(echo "${related_images}" | jq -r '."lvms-must-gather"'))
	TOPOLVM_SHA=$(get_image_sha $(echo "${related_images}" | jq -r '."topolvm-csi"'))

	SKIP_TOPOLVM=0
	if [[ -z "${TOPOLVM_SHA}" || "${TOPOLVM_SHA}" == "null" ]] ; then
		SKIP_TOPOLVM=1
	fi

	# Get the git ref for each image
	OPERAND_IMG="${IMAGE_BASE_URL}/lvm-operator@${OPERAND_SHA}"
	OPERAND_GIT_REF=$(get_image_git_ref "${OPERAND_IMG}")

	MUST_GATHER_IMG="${IMAGE_BASE_URL}/lvms-must-gather@${MUST_GATHER_SHA}"
	MUST_GATHER_GIT_REF=$(get_image_git_ref "${MUST_GATHER_IMG}")

	BUNDLE_IMG="${bundle_image}"

	output_yaml=$(cat <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: Snapshot
metadata:
  name: lvm-operator-${X}-${Y}-${Z}-override
  namespace: logical-volume-manag-tenant
  labels:
    test.appstudio.openshift.io/type: override
spec:
  application: lvm-operator-${X}-${Y}
  components:
    - name: lvm-operator-${X}-${Y}
      containerImage: ${OPERAND_IMG}
      source:
        git:
          url: https://github.com/openshift/lvm-operator
          revision: ${OPERAND_GIT_REF}
    - name: lvms-must-gather-${X}-${Y}
      containerImage: ${MUST_GATHER_IMG}
      source:
        git:
          url: https://github.com/openshift/lvm-operator
          revision: ${MUST_GATHER_GIT_REF}
    - name: lvm-operator-bundle-${X}-${Y}
      containerImage: ${BUNDLE_IMG}
      source:
        git:
          url: https://github.com/openshift/lvm-operator
          revision: ${BUNDLE_GIT_REF}
EOF
	)

	if [ "$SKIP_TOPOLVM" -eq 0 ]; then
		TOPOLVM_IMG="${IMAGE_BASE_URL}/topolvm@${TOPOLVM_SHA}"
		TOPOLVM_GIT_REF=$(get_image_git_ref "${TOPOLVM_IMG}")

		topolvm_obj=$(cat <<-EOF
		{
		  "name": "topolvm-${X}-${Y}",
		  "containerImage": "${TOPOLVM_IMG}",
		  "source": {
		    "git": {
		      "url": "https://github.com/openshift/topolvm",
		      "revision": "${TOPOLVM_GIT_REF}"
		    }
		  }
		}
		EOF
		)

		output_yaml=$(echo "${output_yaml}" | yq ".spec.components += [${topolvm_obj}]")
	fi

	echo "${output_yaml}"
}

main "$@"
