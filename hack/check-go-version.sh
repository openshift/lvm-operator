#!/bin/bash
set -euo pipefail

WORK_DIR=""
JSON_OUTPUT=false
MODE=""
IMAGE_REF=""

MIRROR_MAP=(
    "registry.redhat.io/lvms4/lvms-rhel9-operator=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator"
    "registry.redhat.io/lvms4/lvms-operator-bundle=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle"
    "registry.redhat.io/lvms4/lvms-must-gather-rhel9=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather"
    "registry.redhat.io/lvms4/topolvm-rhel9=quay.io/redhat-user-workloads/logical-volume-manag-tenant/topolvm"
)

usage() {
    echo "Usage: $0 [--json] --image <image-ref> | --bundle <image-ref> | --catalog <image-ref>"
    echo ""
    echo "Check Go toolchain version used to compile binaries in LVMS images."
    echo ""
    echo "Options:"
    echo "  --image <ref>    Check Go version directly on a single container image"
    echo "  --bundle <ref>   Inspect a bundle image and check Go version on its related images"
    echo "  --catalog <ref>  Inspect a catalog image, extract the latest bundle, and check Go versions"
    echo "  --json           Output results as JSON"
    echo "  --help           Show this help message"
    exit 1
}

cleanup() {
    if [[ -n "${WORK_DIR}" && -d "${WORK_DIR}" ]]; then
        rm -rf "${WORK_DIR}"
    fi
}
trap cleanup EXIT

check_tools() {
    for tool in podman go jq yq skopeo; do
        command -v "${tool}" >/dev/null 2>&1 || {
            echo >&2 "Error: '${tool}' is required but not found in PATH"
            exit 1
        }
    done
}

apply_mirror() {
    local image="$1"
    for mapping in "${MIRROR_MAP[@]}"; do
        local source="${mapping%%=*}"
        local mirror="${mapping##*=}"
        if [[ "${image}" == "${source}"@* ]]; then
            echo "${mirror}@${image#*@}"
            return 0
        elif [[ "${image}" == "${source}:"* ]]; then
            echo "${mirror}:${image#*:}"
            return 0
        fi
    done
    echo "${image}"
}

inspect_image_labels() {
    local image="$1"
    local labels
    labels=$(skopeo inspect --no-tags "docker://${image}" 2>/dev/null | jq -r '.Labels') || true
    if [[ -z "${labels}" || "${labels}" == "null" ]]; then
        local mirrored
        mirrored=$(apply_mirror "${image}")
        if [[ "${mirrored}" != "${image}" ]]; then
            labels=$(skopeo inspect --no-tags "docker://${mirrored}" 2>/dev/null | jq -r '.Labels') || true
        fi
    fi
    if [[ -z "${labels}" || "${labels}" == "null" ]]; then
        echo "unknown|unknown"
        return
    fi
    local version commit
    version=$(echo "${labels}" | jq -r '."konflux.additional-tags" // .version // "unknown"')
    commit=$(echo "${labels}" | jq -r '."vcs-ref" // ."org.opencontainers.image.revision" // "unknown"')
    echo "${version}|${commit}"
}

pull_image() {
    local image="$1"
    if podman pull "${image}" >/dev/null 2>&1; then
        echo "${image}"
        return 0
    fi
    local mirrored
    mirrored=$(apply_mirror "${image}")
    if [[ "${mirrored}" != "${image}" ]]; then
        echo >&2 "  Mirror: pulling from ${mirrored}"
        if podman pull "${mirrored}" >/dev/null 2>&1; then
            echo "${mirrored}"
            return 0
        fi
    fi
    echo >&2 "  Error: failed to pull ${image}"
    return 1
}

check_go_version_in_image() {
    local image="$1"
    local label_info img_version img_commit
    label_info=$(inspect_image_labels "${image}")
    img_version=$(echo "${label_info}" | cut -d'|' -f1)
    img_commit=$(echo "${label_info}" | cut -d'|' -f2)

    local pulled_ref
    pulled_ref=$(pull_image "${image}") || return 1

    local cid
    cid=$(podman create "${pulled_ref}" 2>/dev/null) || {
        echo >&2 "  Error: failed to create container from ${pulled_ref}"
        return 1
    }

    local bindir="${WORK_DIR}/bins"
    mkdir -p "${bindir}"

    local found=false
    for path in /lvms /topolvm-controller /topolvm-node /lvmd /hypertopolvm /manager /usr/bin/oc; do
        if podman cp "${cid}:${path}" "${bindir}/" 2>/dev/null; then
            local binname
            binname=$(basename "${path}")
            local version_output
            version_output=$(go version "${bindir}/${binname}" 2>/dev/null) || continue
            local go_ver
            go_ver=$(echo "${version_output}" | sed "s|${bindir}/${binname}: ||")
            echo "${image}|${path}|${go_ver}|${img_version}|${img_commit}"
            found=true
            rm -f "${bindir}/${binname}"
        fi
    done

    rm -rf "${bindir}"
    podman rm "${cid}" >/dev/null 2>&1 || true

    if [[ "${found}" == "false" ]]; then
        echo "${image}|(none)|(no Go binaries found)|${img_version}|${img_commit}"
    fi
}

is_lvms_image() {
    local image="$1"
    [[ "${image}" == *"lvms4/"* || "${image}" == *"logical-volume-manag-tenant/"* ]]
}

get_related_images_from_bundle() {
    local image="$1"
    local pulled_ref
    pulled_ref=$(pull_image "${image}") || return 1

    local cid
    cid=$(podman create "${pulled_ref}" 2>/dev/null)

    local manifests="${WORK_DIR}/manifests"
    mkdir -p "${manifests}"
    podman cp "${cid}:/manifests" "${manifests}/" 2>/dev/null
    podman rm "${cid}" >/dev/null 2>&1

    local csv
    csv=$(find "${manifests}" -name "*.clusterserviceversion.yaml" | head -1)
    if [[ -z "${csv}" ]]; then
        echo >&2 "Error: no CSV found in bundle"
        return 1
    fi

    yq -o=json '.spec.relatedImages[] | .image' "${csv}" 2>/dev/null | tr -d '"'
    rm -rf "${manifests}"
}

get_related_images_from_catalog() {
    local image="$1"
    local pulled_ref
    pulled_ref=$(pull_image "${image}") || return 1

    local cid
    cid=$(podman create "${pulled_ref}" 2>/dev/null)

    local catalog_json="${WORK_DIR}/catalog.json"
    podman cp "${cid}:/configs/lvms-operator/catalog.json" "${catalog_json}" 2>/dev/null
    podman rm "${cid}" >/dev/null 2>&1

    if [[ ! -f "${catalog_json}" ]]; then
        echo >&2 "Error: catalog.json not found in image"
        return 1
    fi

    local latest_bundle
    latest_bundle=$(jq -s '[.[] | select(.schema == "olm.bundle")] | sort_by(.name | split(".v") | .[1] | split(".") | map(tonumber)) | last' "${catalog_json}")

    local bundle_name
    bundle_name=$(echo "${latest_bundle}" | jq -r '.name')
    echo >&2 "Latest bundle: ${bundle_name}"

    echo "${latest_bundle}" | jq -r '.relatedImages[].image'
    rm -f "${catalog_json}"
}

main() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --image)
                MODE="image"
                IMAGE_REF="$2"
                shift 2
                ;;
            --bundle)
                MODE="bundle"
                IMAGE_REF="$2"
                shift 2
                ;;
            --catalog)
                MODE="catalog"
                IMAGE_REF="$2"
                shift 2
                ;;
            --json)
                JSON_OUTPUT=true
                shift
                ;;
            --help|-h)
                usage
                ;;
            *)
                echo "Unknown option: $1"
                usage
                ;;
        esac
    done

    if [[ -z "${MODE}" || -z "${IMAGE_REF}" ]]; then
        usage
    fi

    check_tools
    WORK_DIR=$(mktemp -d)

    local input_labels input_version input_commit
    input_labels=$(inspect_image_labels "${IMAGE_REF}")
    input_version=$(echo "${input_labels}" | cut -d'|' -f1)
    input_commit=$(echo "${input_labels}" | cut -d'|' -f2)
    echo >&2 "${MODE^} version: ${input_version} (commit: ${input_commit:0:12})"

    local images=()
    if [[ "${MODE}" == "image" ]]; then
        images+=("${IMAGE_REF}")
    elif [[ "${MODE}" == "bundle" ]]; then
        echo >&2 "Extracting image references from bundle: ${IMAGE_REF}"
        while IFS= read -r img; do
            images+=("${img}")
        done < <(get_related_images_from_bundle "${IMAGE_REF}")
        if [[ ${#images[@]} -eq 0 ]]; then
            echo >&2 "Error: no related images found. Is this really a bundle image?"
            exit 1
        fi
    else
        echo >&2 "Extracting image references from catalog: ${IMAGE_REF}"
        while IFS= read -r img; do
            images+=("${img}")
        done < <(get_related_images_from_catalog "${IMAGE_REF}")
        if [[ ${#images[@]} -eq 0 ]]; then
            echo >&2 "Error: no related images found. Is this really a catalog image?"
            exit 1
        fi
    fi

    local results=()
    for img in "${images[@]}"; do
        if [[ "${MODE}" != "image" ]]; then
            if ! is_lvms_image "${img}"; then
                continue
            fi
            if [[ "${img}" == *"operator-bundle"* ]]; then
                continue
            fi
        fi
        echo >&2 "Checking: ${img}"
        while IFS= read -r result; do
            results+=("${result}")
        done < <(check_go_version_in_image "${img}")
    done

    if [[ "${JSON_OUTPUT}" == "true" ]]; then
        local json_file="${WORK_DIR}/results.jsonl"
        for result in "${results[@]}"; do
            local img bin ver img_ver img_commit
            img=$(echo "${result}" | cut -d'|' -f1)
            bin=$(echo "${result}" | cut -d'|' -f2)
            ver=$(echo "${result}" | cut -d'|' -f3)
            img_ver=$(echo "${result}" | cut -d'|' -f4)
            img_commit=$(echo "${result}" | cut -d'|' -f5)
            jq -n \
                --arg image "${img}" \
                --arg version "${img_ver}" \
                --arg commit "${img_commit}" \
                --arg binary "${bin}" \
                --arg go_version "${ver}" \
                '{image: $image, version: $version, commit: $commit, binary: $binary, go_version: $go_version}' \
                >> "${json_file}"
        done
        jq -s '.' "${json_file}"
    else
        printf "%-35s %-15s %-14s %-18s %s\n" "IMAGE" "VERSION" "COMMIT" "BINARY" "GO VERSION"
        printf "%-35s %-15s %-14s %-18s %s\n" "-----" "-------" "------" "------" "----------"
        for result in "${results[@]}"; do
            local img bin ver img_ver img_commit
            img=$(echo "${result}" | cut -d'|' -f1)
            bin=$(echo "${result}" | cut -d'|' -f2)
            ver=$(echo "${result}" | cut -d'|' -f3)
            img_ver=$(echo "${result}" | cut -d'|' -f4)
            img_commit=$(echo "${result}" | cut -d'|' -f5)
            local short_img="${img}"
            short_img="${short_img#registry.redhat.io/}"
            short_img="${short_img#registry.stage.redhat.io/}"
            short_img="${short_img#quay.io/redhat-user-workloads/logical-volume-manag-tenant/}"
            if [[ "${short_img}" == *"@sha256:"* ]]; then
                short_img="${short_img%%@*}"
            fi
            printf "%-35s %-15s %-14s %-18s %s\n" "${short_img}" "${img_ver}" "${img_commit:0:12}" "${bin}" "${ver}"
        done
    fi
}

main "$@"
