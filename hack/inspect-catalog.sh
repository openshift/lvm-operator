#!/bin/bash
set -euo pipefail

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS] CATALOG_IMAGE

Extract and display all bundles from an OLM file-based catalog image.

Arguments:
  CATALOG_IMAGE    Catalog image reference (tag or digest)

Options:
  --json           Output as JSON array (includes commit IDs)
  --arch ARCH      Platform architecture (default: amd64)
  --package NAME   Filter to a specific package
  --version VER    Filter to a specific version (e.g., 4.19.3 or v4.19.3)
  --no-commits     Skip fetching commit IDs (faster)
  -h, --help       Show this help

Examples:
  $(basename "$0") quay.io/org/catalog@sha256:abc123...
  $(basename "$0") --json quay.io/org/catalog:latest
  $(basename "$0") --package lvms-operator --version 4.19.3 IMAGE
EOF
    exit "${1:-0}"
}

OUTPUT_JSON=false
ARCH="amd64"
PACKAGE_FILTER=""
VERSION_FILTER=""
CATALOG_IMAGE=""
FETCH_COMMITS=true

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
        --no-commits)
            FETCH_COMMITS=false
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
    media_type=$(echo "${raw}" | \
        jq -r '.mediaType // .schemaVersion' 2>/dev/null)

    case "${media_type}" in
        *index*|*list*)
            local digest
            digest=$(echo "${raw}" | jq -r \
                --arg arch "${arch}" \
                '[.manifests[] |
                    select(.platform.architecture == $arch) |
                    .digest] | first' \
                2>/dev/null)
            if [[ -z "${digest}" ]]; then
                echo "Error: no manifest for arch ${arch}" >&2
                exit 1
            fi
            local base
            base=$(echo "${image}" | \
                sed 's/@sha256:.*//; s/:.*$//')
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
skopeo copy "docker://${RESOLVED_IMAGE}" \
    "dir://${IMGDIR}" >/dev/null 2>&1

FBCDIR="${TMPDIR}/fbc"
mkdir -p "${FBCDIR}"

LAYERS=$(jq -r \
    '.layers[].digest | sub("sha256:";"")' \
    "${IMGDIR}/manifest.json")
for layer in ${LAYERS}; do
    has_configs=$(tar tf "${IMGDIR}/${layer}" \
        2>/dev/null | grep -c "^configs/" || true)
    if [[ "${has_configs}" -gt 0 ]]; then
        tar xf "${IMGDIR}/${layer}" \
            -C "${FBCDIR}" "configs" 2>/dev/null || true
    fi
done

CATALOG_FILES=$(find "${FBCDIR}" \
    -name "catalog.json" -type f 2>/dev/null)
if [[ -z "${CATALOG_FILES}" ]]; then
    echo "Error: no catalog.json found in image" >&2
    exit 1
fi

ALL_ENTRIES="${TMPDIR}/all_entries.json"
for f in ${CATALOG_FILES}; do
    cat "${f}"
done | jq -s '.' > "${ALL_ENTRIES}"

JQ_QUERY_FILE="${TMPDIR}/query.jq"
cat > "${JQ_QUERY_FILE}" << 'JQEOF'
def channel_entries:
    [.[] | select(.schema == "olm.channel") |
        . as $ch | .entries[]? |
        {channel: $ch.name, bundle: .name, replaces: .replaces,
         skips: (.skips // []), skipRange: .skipRange}];

def get_version:
    [(.properties // [])[] |
        select(.type == "olm.package") | .value.version
    ] | first // "";

. as $all |
channel_entries as $channels |
[.[] | select(.schema == "olm.bundle") |
    select(if $pkg != "" then .package == $pkg else true end) |
    get_version as $bv |
    select(if $ver != "" then
        ($bv == $ver) or (.name | endswith("v" + $ver))
    else true end) |
    . as $bundle |
    {
        package: .package,
        name: .name,
        version: $bv,
        image: .image,
        channels: ([($channels[] |
            select(.bundle == $bundle.name) | .channel)] | unique),
        upgrade_info: [($channels[] |
            select(.bundle == $bundle.name) |
            {channel: .channel, replaces: .replaces,
             skips: .skips, skipRange: .skipRange})],
        relatedImages: [(.relatedImages // [])[] |
            {name: .name, image: .image}]
    }
] | sort_by(.package, .name)
JQEOF

build_output() {
    local package_filter="$1"
    local version_filter="$2"

    jq --arg pkg "${package_filter}" \
       --arg ver "${version_filter}" \
       -f "${JQ_QUERY_FILE}" "${ALL_ENTRIES}"
}

echo "Extracting bundle information..." >&2

RESULT=$(build_output "${PACKAGE_FILTER}" "${VERSION_FILTER}")

# Collect all unique images and fetch commit IDs in parallel
COMMITS_FILE="${TMPDIR}/commits.json"

if [[ "${FETCH_COMMITS}" == "true" ]]; then
    IMAGES_FILE="${TMPDIR}/all_images.txt"
    echo "${RESULT}" | jq -r \
        '([.[].image] + [.[].relatedImages[].image]) | unique | .[]' \
        > "${IMAGES_FILE}"

    IMAGE_COUNT=$(wc -l < "${IMAGES_FILE}")
    echo "Fetching commit IDs for ${IMAGE_COUNT} images..." >&2

    COMMITS_RAW="${TMPDIR}/commits_raw.txt"
    : > "${COMMITS_RAW}"

    fetch_commit() {
        local image="$1"
        local output_file="$2"
        local commit
        commit=$(skopeo inspect "docker://${image}" 2>/dev/null | \
            jq -r '.Labels["vcs-ref"] // empty' 2>/dev/null || true)
        if [[ -n "${commit}" ]]; then
            echo "${image} ${commit}" >> "${output_file}"
        fi
    }
    export -f fetch_commit

    xargs -a "${IMAGES_FILE}" -P 8 -I {} \
        bash -c 'fetch_commit "$@"' _ {} "${COMMITS_RAW}"

    # Build JSON lookup from raw results
    jq -Rn '[inputs | split(" ") | {(.[0]): .[1]}] | add // {}' \
        "${COMMITS_RAW}" > "${COMMITS_FILE}"

    echo "Done." >&2
else
    echo '{}' > "${COMMITS_FILE}"
fi

# Merge commit IDs into result
RESULT_WITH_COMMITS=$(jq --slurpfile commits "${COMMITS_FILE}" '
    ($commits[0] // {}) as $cm |
    [.[] | . + {
        commit: ($cm[.image] // null),
        relatedImages: [.relatedImages[] | . + {
            commit: ($cm[.image] // null)
        }]
    }]
' <<< "${RESULT}")

if [[ "${OUTPUT_JSON}" == "true" ]]; then
    echo "${RESULT_WITH_COMMITS}" | jq '.'
    exit 0
fi

# Compact human-readable output grouped by package > channel > bundle
echo ""
echo "${RESULT_WITH_COMMITS}" | jq -r '
def short_sha:
    if . then
        split("@sha256:") |
        if length > 1 then .[1][:12] else .[0] end
    else "------------" end;

def short_commit:
    if . then .[:12] else "------------" end;

def strip_prefix:
    ltrimstr("lvms-operator.");

def pad($n):
    . + (" " * ([$n - length, 0] | max));

def format_upgrade:
    [
        (if .replaces then
            "replaces " + (.replaces | strip_prefix)
        else null end),
        (if (.skips | length) > 0 then
            "skips " + ([.skips[] |
                strip_prefix] | join(","))
        else null end),
        (if .skipRange then
            "range " + .skipRange
        else null end)
    ] | map(select(. != null)) | join("  ");

def bundle_line:
    .image as $sha | .commit as $c |
    "  [bundle] \(.name | pad(28)) " +
    "\($sha | short_sha)  " +
    "commit:\($c | short_commit)";

def related_line:
    "    \(.name | pad(20)) " +
    "\(.image | short_sha)  " +
    "commit:\(.commit | short_commit)";

group_by(.package)[] |
    .[0].package as $pkg |
    "Package: \($pkg)",
    (group_by(.channels[0])[] |
        .[0].channels[0] as $chan |
        "",
        "  Channel: \($chan)",
        "  \("-" * 70)",
        (sort_by(.version |
            split(".") | map(tonumber? // 0))[] |
            .image as $bundle_img |
            (.upgrade_info[0] |
                format_upgrade) as $ug |
            bundle_line +
            (if ($ug | length) > 0 then
                "\n    \($ug)"
            else "" end),
            (.relatedImages[] |
                select(.image != $bundle_img) |
                related_line
            )
        )
    ),
    ""
'
