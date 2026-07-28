#!/bin/bash
set -euo pipefail

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS] CATALOG_IMAGE

Extract and display all bundles from an OLM file-based catalog image.

Arguments:
  CATALOG_IMAGE    Catalog image reference (tag or digest)

Options:
  --json           Output as JSON array
  --arch ARCH      Platform architecture (default: amd64)
  --package NAME   Filter to a specific package
  --version VER    Filter to a specific version (e.g., 4.19.3 or v4.19.3)
  --labels         Fetch image labels (vcs-ref, build-date) via skopeo inspect per bundle
  -h, --help       Show this help

Examples:
  $(basename "$0") quay.io/org/catalog@sha256:abc123...
  $(basename "$0") --json --labels quay.io/org/catalog:latest
  $(basename "$0") --package lvms-operator --version 4.19.3 quay.io/org/catalog@sha256:abc123...
EOF
    exit "${1:-0}"
}

OUTPUT_JSON=false
ARCH="amd64"
PACKAGE_FILTER=""
VERSION_FILTER=""
FETCH_LABELS=false
CATALOG_IMAGE=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --json)
            OUTPUT_JSON=true
            shift
            ;;
        --arch)
            ARCH="$2"
            shift 2
            ;;
        --package)
            PACKAGE_FILTER="$2"
            shift 2
            ;;
        --version)
            VERSION_FILTER="${2#v}"
            shift 2
            ;;
        --labels)
            FETCH_LABELS=true
            shift
            ;;
        -h|--help)
            usage 0
            ;;
        -*)
            echo "Error: unknown option $1" >&2
            usage 1
            ;;
        *)
            CATALOG_IMAGE="$1"
            shift
            ;;
    esac
done

if [[ -z "${CATALOG_IMAGE}" ]]; then
    echo "Error: catalog image argument required" >&2
    usage 1
fi

for cmd in skopeo jq tar; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "Error: ${cmd} is required but not found" >&2
        exit 1
    fi
done

TMPDIR=$(mktemp -d)
trap 'rm -rf "${TMPDIR}"' EXIT

resolve_arch_digest() {
    local image="$1"
    local arch="$2"

    local raw
    raw=$(skopeo inspect --raw "docker://${image}" 2>/dev/null)

    local media_type
    media_type=$(echo "${raw}" | jq -r '.mediaType // .schemaVersion' 2>/dev/null)

    case "${media_type}" in
        *index*|*list*)
            local digest
            digest=$(echo "${raw}" | jq -r \
                --arg arch "${arch}" \
                '.manifests[] | select(.platform.architecture == $arch) | .digest' 2>/dev/null | head -1)
            if [[ -z "${digest}" ]]; then
                echo "Error: no manifest found for architecture ${arch}" >&2
                exit 1
            fi
            local base
            base=$(echo "${image}" | sed 's/@sha256:.*//; s/:.*$//')
            echo "${base}@${digest}"
            ;;
        *)
            echo "${image}"
            ;;
    esac
}

echo "Resolving ${ARCH} manifest..." >&2
RESOLVED_IMAGE=$(resolve_arch_digest "${CATALOG_IMAGE}" "${ARCH}")
echo "Pulling image layers..." >&2

IMGDIR="${TMPDIR}/image"
mkdir -p "${IMGDIR}"
skopeo copy "docker://${RESOLVED_IMAGE}" "dir://${IMGDIR}" >/dev/null 2>&1

FBCDIR="${TMPDIR}/fbc"
mkdir -p "${FBCDIR}"

LAYERS=$(jq -r '.layers[].digest | sub("sha256:";"")' "${IMGDIR}/manifest.json")
for layer in ${LAYERS}; do
    has_configs=$(tar tf "${IMGDIR}/${layer}" 2>/dev/null | grep -c "^configs/" || true)
    if [[ "${has_configs}" -gt 0 ]]; then
        tar xf "${IMGDIR}/${layer}" -C "${FBCDIR}" "configs" 2>/dev/null || true
    fi
done

CATALOG_FILES=$(find "${FBCDIR}" -name "catalog.json" -type f 2>/dev/null)
if [[ -z "${CATALOG_FILES}" ]]; then
    echo "Error: no catalog.json found in image" >&2
    exit 1
fi

ALL_ENTRIES="${TMPDIR}/all_entries.json"
for f in ${CATALOG_FILES}; do
    cat "${f}"
done | jq -s '.' > "${ALL_ENTRIES}"

build_output() {
    local package_filter="$1"
    local version_filter="$2"

    jq --arg pkg "${package_filter}" --arg ver "${version_filter}" '
    def channel_entries:
        [.[] | select(.schema == "olm.channel") |
            . as $ch |
            .entries[]? |
            {channel: $ch.name, bundle: .name, replaces: .replaces, skips: (.skips // []), skipRange: .skipRange}
        ];

    . as $all |
    channel_entries as $channels |

    [.[] | select(.schema == "olm.bundle") |
        select(if $pkg != "" then .package == $pkg else true end) |
        ((.properties // [])[] | select(.type == "olm.package") | .value.version) as $bundle_version |
        select(if $ver != "" then ($bundle_version == $ver or .name == (".*v" + $ver) or (.name | endswith("v" + $ver))) else true end) |
        . as $bundle |
        {
            package: .package,
            name: .name,
            image: .image,
            channels: [($channels[] | select(.bundle == $bundle.name) | .channel)] | unique,
            upgrade_info: [($channels[] | select(.bundle == $bundle.name) |
                {channel: .channel, replaces: .replaces, skips: .skips, skipRange: .skipRange})],
            relatedImages: [(.relatedImages // [])[] | {name: .name, image: .image}],
            properties: [(.properties // [])[] |
                select(.type == "olm.package") | .value]
        }
    ] | sort_by(.package, .name)
    ' "${ALL_ENTRIES}"
}

echo "Extracting bundle information..." >&2

RESULT=$(build_output "${PACKAGE_FILTER}" "${VERSION_FILTER}")

fetch_image_labels() {
    local image="$1"
    skopeo inspect "docker://${image}" 2>/dev/null | jq '{
        "vcs-ref": .Labels["vcs-ref"],
        "org.opencontainers.image.revision": .Labels["org.opencontainers.image.revision"],
        "org.opencontainers.image.created": .Labels["org.opencontainers.image.created"],
        "build-date": .Labels["build-date"],
        "version": .Labels["version"]
    } | with_entries(select(.value != null))' 2>/dev/null || echo '{}'
}

if [[ "${FETCH_LABELS}" == "true" ]]; then
    BUNDLE_COUNT=$(echo "${RESULT}" | jq 'length')
    echo "Fetching labels for ${BUNDLE_COUNT} bundle(s)..." >&2

    LABELS_FILE="${TMPDIR}/labels.json"
    echo '{}' > "${LABELS_FILE}"

    for image in $(echo "${RESULT}" | jq -r '.[].image'); do
        echo "  Inspecting ${image##*@}..." >&2
        labels=$(fetch_image_labels "${image}")
        jq --arg img "${image}" --argjson labels "${labels}" \
            '. + {($img): $labels}' "${LABELS_FILE}" > "${LABELS_FILE}.tmp" \
            && mv "${LABELS_FILE}.tmp" "${LABELS_FILE}"
    done

    RESULT=$(echo "${RESULT}" | jq --slurpfile labels "${LABELS_FILE}" '
        . as $bundles |
        ($labels[0] // {}) as $all_labels |
        [.[] | . + {labels: ($all_labels[.image] // {})}]
    ')
fi

if [[ "${OUTPUT_JSON}" == "true" ]]; then
    echo "${RESULT}" | jq '.'
    exit 0
fi

echo "${RESULT}" | jq -r '
    .[] |
    "================================================================================",
    "Package:  \(.package)",
    "Bundle:   \(.name)",
    "Image:    \(.image)",
    "Channels: \(.channels | join(", "))",
    (if .labels and (.labels | length) > 0 then
        "Labels:",
        (.labels | to_entries[] | "  \(.key): \(.value)")
    else empty end),
    "",
    (if (.upgrade_info | length) > 0 then
        (.upgrade_info[] |
            "  Channel \(.channel):" +
            (if .replaces then "\n    Replaces:  \(.replaces)" else "" end) +
            (if (.skips | length) > 0 then "\n    Skips:     \(.skips | join(", "))" else "" end) +
            (if .skipRange then "\n    SkipRange: \(.skipRange)" else "" end)
        )
    else "" end),
    "",
    "Related Images:",
    (.relatedImages[] |
        "  \(if .name != "" then "[\(.name)] " else "" end)\(.image)"
    ),
    ""
'
