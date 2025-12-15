#!/bin/bash
#
# get-bundle-for-catalog.sh
#
# Extract bundle image reference from an OLM catalog image
#
# Usage: get-bundle-for-catalog.sh CATALOG_IMAGE [OPTIONS]
#
# Examples:
#   get-bundle-for-catalog.sh quay.io/org/catalog@sha256:abc123
#   get-bundle-for-catalog.sh --snapshot lvm-operator-catalog-4-18-t2sc5
#   get-bundle-for-catalog.sh --version 4.18 --env staging
#

set -euo pipefail

# ============================================================================
# CONSTANTS
# ============================================================================
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly DEFAULT_NAMESPACE="logical-volume-manag-tenant"
readonly TEMP_DIR="/tmp/catalog-extraction-$$"

# ============================================================================
# GLOBALS
# ============================================================================
NAMESPACE="${DEFAULT_NAMESPACE}"
CATALOG_IMAGE=""
SNAPSHOT_NAME=""
VERSION=""
ENVIRONMENT=""
OUTPUT_FORMAT="text"
DEBUG=false
CLEANUP=true

# ============================================================================
# HELPER FUNCTIONS
# ============================================================================

usage() {
    cat <<EOF
Usage: $(basename "$0") [CATALOG_IMAGE] [OPTIONS]

Extract bundle image reference from an OLM catalog image.

Positional Arguments:
    CATALOG_IMAGE         Full catalog image reference (e.g., quay.io/org/catalog@sha256:...)

Options:
    -s, --snapshot NAME   Get catalog from snapshot name
    -v, --version VER     Get latest catalog snapshot for version (e.g., 4.18)
    -e, --env ENV         Filter by environment: staging, production (optional with --version)
    -n, --namespace NS    Override namespace (default: ${DEFAULT_NAMESPACE})
    -f, --format FORMAT   Output format: text, json, yaml (default: text)
    --no-cleanup          Don't cleanup temporary files
    --debug               Enable debug output
    -h, --help            Show this help message

Environment Filtering:
    staging      Snapshots that passed enterprise-contract-push-staging tests
    production   Snapshots added to global candidate list (production-ready)
    (no filter)  Latest push event snapshot

Examples:
    # Direct catalog image reference
    $(basename "$0") quay.io/redhat-user-workloads/org/catalog@sha256:abc123

    # From snapshot name
    $(basename "$0") --snapshot lvm-operator-catalog-4-18-t2sc5

    # Latest snapshot for version (any environment)
    $(basename "$0") --version 4.18

    # Latest staging snapshot
    $(basename "$0") --version 4.18 --env staging

    # Latest production snapshot
    $(basename "$0") --version 4.21 --env production

    # JSON output
    $(basename "$0") --version 4.18 --env staging --format json

EOF
}

debug() {
    if [ "$DEBUG" = true ]; then
        echo "[DEBUG] $*" >&2
    fi
}

error() {
    echo "ERROR: $*" >&2
}

warn() {
    echo "WARNING: $*" >&2
}

info() {
    echo "$*"
}

cleanup() {
    if [ "$CLEANUP" = true ] && [ -d "$TEMP_DIR" ]; then
        debug "Cleaning up temporary directory: $TEMP_DIR"
        rm -rf "$TEMP_DIR"
    fi
}

trap cleanup EXIT

# ============================================================================
# DEPENDENCY CHECKS
# ============================================================================

check_dependencies() {
    local missing=()

    if ! command -v oc >/dev/null 2>&1; then
        missing+=("oc")
    fi

    if ! command -v jq >/dev/null 2>&1; then
        missing+=("jq")
    fi

    # Check if oc ka plugin is available
    if ! oc ka --help >/dev/null 2>&1; then
        missing+=("oc-ka-plugin")
    fi

    if [ ${#missing[@]} -gt 0 ]; then
        error "Missing required tools: ${missing[*]}"
        info ""
        info "Installation instructions:"
        for tool in "${missing[@]}"; do
            case "$tool" in
                oc)
                    info "  oc: https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html"
                    ;;
                oc-ka-plugin)
                    info "  oc ka plugin: Install the KubeArchive oc plugin"
                    ;;
                jq)
                    info "  jq: https://stedolan.github.io/jq/download/"
                    ;;
            esac
        done
        exit 1
    fi

    debug "All required dependencies are available"
}

check_auth() {
    if ! oc whoami >/dev/null 2>&1; then
        error "Not authenticated with Konflux"
        info ""
        info "Please authenticate first:"
        info "  oc login --web https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443/"
        exit 1
    fi

    debug "Authentication verified: $(oc whoami)"
}

setup_kubearchive() {
    # Set KubeArchive host for oc ka plugin
    export KUBECTL_PLUGIN_KA_HOST="https://kubearchive-api-server-product-kubearchive.apps.$(oc whoami --show-console | sed -E 's/.*\.apps\.//')"
    debug "KubeArchive host configured: ${KUBECTL_PLUGIN_KA_HOST}"
}

# ============================================================================
# QUERY FUNCTIONS
# ============================================================================

get_catalog_from_snapshot() {
    local snapshot="$1"

    debug "Getting catalog image from snapshot: $snapshot (using oc ka for archived + active)"

    local catalog_image
    catalog_image=$(oc ka get snapshot "$snapshot" -n "${NAMESPACE}" \
        -o json 2>/dev/null | \
        jq -r '.items[0].spec.components[] | select(.name | contains("catalog")) | .containerImage' | \
        head -1)

    if [ -z "$catalog_image" ]; then
        error "No catalog component found in snapshot: $snapshot"
        return 1
    fi

    echo "$catalog_image"
}

get_latest_catalog_snapshot() {
    local version="$1"
    local env="$2"

    debug "Getting latest catalog snapshot for version=$version, env=${env:-any} (using oc ka releases with label filtering)"

    local y_stream="${version##*.}"
    local component_label="lvm-operator-catalog-4-${y_stream}"

    # Determine the release plan name based on environment
    # Pattern: lvm-operator-catalog-{env}-releaseplan-4-{y_stream}
    local release_plan_pattern
    if [ -n "$env" ]; then
        release_plan_pattern="lvm-operator-catalog-${env}-releaseplan-4-${y_stream}"
        debug "Looking for releases with plan: $release_plan_pattern"
    else
        release_plan_pattern="lvm-operator-catalog-.*-releaseplan-4-${y_stream}"
        debug "Looking for releases matching plan pattern: $release_plan_pattern"
    fi

    # Query releases to find the latest snapshot for this version and environment
    # This is the authoritative source - Release objects track what's actually been released
    # Filter by component label for better performance
    local snapshot
    snapshot=$(oc ka get releases -n "${NAMESPACE}" \
        -l "appstudio.openshift.io/component=${component_label}" \
        -o json 2>/dev/null | \
        jq -r --arg plan "$release_plan_pattern" \
            '.items[] |
        select(.spec.releasePlan != null) |
        select(.spec.releasePlan | test($plan)) |
        {snapshot: .spec.snapshot, timestamp: .metadata.creationTimestamp}' 2>/dev/null | \
        jq -s 'sort_by(.timestamp) | reverse | .[0].snapshot' 2>/dev/null | \
        tr -d '"')

    if [ -z "$snapshot" ] || [ "$snapshot" = "null" ]; then
        error "No catalog snapshot found for version=$version, env=${env:-any} (release plan: $release_plan_pattern)"
        debug "Tried release plan pattern: $release_plan_pattern with component label: $component_label in namespace: $NAMESPACE"
        return 1
    fi

    debug "Found snapshot: $snapshot"
    echo "$snapshot"
}

# ============================================================================
# EXTRACTION FUNCTIONS
# ============================================================================

extract_bundle_from_catalog() {
    local catalog_image="$1"

    debug "Extracting bundle information from catalog: $catalog_image"

    # Create temp directory
    mkdir -p "$TEMP_DIR"

    # Extract catalog configs
    debug "Extracting catalog configs to: $TEMP_DIR"
    if ! oc image extract "$catalog_image" --path /configs/:"$TEMP_DIR" --confirm >/dev/null 2>&1; then
        error "Failed to extract catalog image"
        return 1
    fi

    # Find the bundle entry - select the latest version using semantic version sorting
    local bundle_info
    bundle_info=$(find "$TEMP_DIR" -name "*.json" -type f -exec cat {} \; | \
        jq -r 'select(.schema == "olm.bundle") | {
        image: .image,
        name: .name,
        package: .package,
        version: (.properties[] | select(.type == "olm.package") | .value.version)
        }' | \
        jq -s 'sort_by(.version | split(".") | map(tonumber)) | reverse | .[0]')

    if [ -z "$bundle_info" ] || [ "$bundle_info" = "null" ]; then
        error "No bundle information found in catalog"
        return 1
    fi

    echo "$bundle_info"
}

find_bundle_snapshot() {
    local bundle_image="$1"
    local bundle_version="$2"
    local catalog_snapshot="$3"

    debug "Finding bundle snapshot for image: $bundle_image (version: $bundle_version)"

    # Extract the SHA digest from the bundle image
    # Format: registry.redhat.io/lvms4/lvms-operator-bundle@sha256:abc123...
    local bundle_sha
    bundle_sha=$(echo "$bundle_image" | sed -n 's/.*@sha256:\(.*\)/\1/p')

    if [ -z "$bundle_sha" ]; then
        debug "No SHA256 digest found in bundle image reference"
        return 1
    fi

    debug "Looking for bundle snapshot with SHA: $bundle_sha"

    # Use the provided version to filter snapshots
    # Format: 4.20.0 -> extract 4.20
    local version_hint=""
    if [ -n "$bundle_version" ]; then
        version_hint=$(echo "$bundle_version" | sed -n 's/^\([0-9]\+\.[0-9]\+\).*/\1/p')
        debug "Using version hint from bundle: $version_hint"
    fi

    # Get catalog snapshot creation time if provided (for sorting by age)
    local catalog_time=""
    if [ -n "$catalog_snapshot" ]; then
        catalog_time=$(oc ka get snapshot "$catalog_snapshot" -n "${NAMESPACE}" -o json 2>/dev/null | \
            jq -r '.items[0].metadata.creationTimestamp // empty')
        if [ -n "$catalog_time" ]; then
            debug "Catalog snapshot created at: $catalog_time"
        fi
    fi

    # Search for snapshots that contain this bundle SHA
    # The snapshot will have format: lvm-operator-4-YY-XXXXX (without "catalog")
    # The image reference in the snapshot will be quay.io but with same SHA
    # We need to search across different component labels since the bundle might be in different component snapshots
    local snapshot_name=""

    # Try searching with application label filter if we have a version hint
    # The bundle snapshots have format: lvm-operator-4-YY-XXXXX
    # and belong to application: lvm-operator-4-YY
    #
    # Strategy:
    # 1. First check if there's a Release object that references a snapshot with this SHA
    # 2. Otherwise, fall back to timestamp sorting
    if [ -n "$version_hint" ]; then
        local y_stream="${version_hint##*.}"
        local app_label="lvm-operator-4-${y_stream}"

        debug "Trying application label: $app_label"

        # First, try to find a Release object that references a snapshot with this bundle SHA
        # Release objects are the authoritative source for what was actually released
        # Strategy: Get all snapshots with matching SHA, then check which are in Releases
        local bundle_component="lvm-operator-bundle-4-${y_stream}"
        debug "Checking for Release objects with component: $bundle_component"

        # Get all snapshot names that have this SHA
        local matching_snapshots
        matching_snapshots=$(oc ka get snapshots -n "${NAMESPACE}" \
            -l "appstudio.openshift.io/application=${app_label}" \
            -o json 2>/dev/null | \
            jq -r --arg sha "$bundle_sha" --arg prefix "lvm-operator-4-${y_stream}-" '
                .items[] |
                select(.metadata.name | startswith($prefix)) |
                select(.spec.components[]? | .containerImage | contains($sha)) |
                .metadata.name
            ')

        # Check if any of these snapshots are referenced by a Release
        if [ -n "$matching_snapshots" ]; then
            debug "Found $(echo "$matching_snapshots" | wc -l) snapshots with matching SHA, checking Releases"
            snapshot_name=$(oc ka get releases -n "${NAMESPACE}" \
                -l "appstudio.openshift.io/component=${bundle_component}" \
                -o json 2>/dev/null | \
                jq -r --arg snapshots "$(echo "$matching_snapshots" | tr '\n' '|' | sed 's/|$//')" '
                    .items[] |
                    select(.spec.snapshot != null) |
                    select(.spec.snapshot | test($snapshots)) |
                    .spec.snapshot
                ' | head -1)

            if [ -n "$snapshot_name" ]; then
                debug "Found bundle snapshot via Release: $snapshot_name"
            fi
        fi

        # If not found via Release, use timestamp sorting
        if [ -z "$snapshot_name" ]; then
            if [ -n "$catalog_time" ]; then
                debug "No Release found, searching for bundle snapshot created before catalog"
                snapshot_name=$(oc ka get snapshots -n "${NAMESPACE}" \
                    -l "appstudio.openshift.io/application=${app_label}" \
                    -o json 2>/dev/null | \
                    jq -r --arg sha "$bundle_sha" \
                        --arg prefix "lvm-operator-4-${y_stream}-" \
                        --arg catalog_time "$catalog_time" '
                        .items[] |
                        select(.metadata.name | startswith($prefix)) |
                        select(.spec.components[]? | .containerImage | contains($sha)) |
                        select(.metadata.creationTimestamp <= $catalog_time) |
                        {name: .metadata.name, time: .metadata.creationTimestamp}
                    ' | jq -s 'sort_by(.time) | reverse | .[0].name // empty' | tr -d '"')
            else
                snapshot_name=$(oc ka get snapshots -n "${NAMESPACE}" \
                    -l "appstudio.openshift.io/application=${app_label}" \
                    -o json 2>/dev/null | \
                    jq -r --arg sha "$bundle_sha" --arg prefix "lvm-operator-4-${y_stream}-" '
                        .items[] |
                        select(.metadata.name | startswith($prefix)) |
                        select(.spec.components[]? | .containerImage | contains($sha)) |
                        {name: .metadata.name, time: .metadata.creationTimestamp}
                    ' | jq -s 'sort_by(.time) | reverse | .[0].name // empty' | tr -d '"')
            fi
        else
            debug "Found bundle snapshot via Release object"
        fi
    fi

    # Fallback to unfiltered search if still not found
    if [ -z "$snapshot_name" ]; then
        debug "Trying unfiltered search (may be slower)"
        snapshot_name=$(oc ka get snapshots -n "${NAMESPACE}" -o json 2>/dev/null | \
            jq -r --arg sha "$bundle_sha" '
                .items[] |
                select(.metadata.name | startswith("lvm-operator-4-")) |
                select(.metadata.name | contains("catalog") | not) |
                select(.spec.components[]? | .containerImage | contains($sha)) |
                {name: .metadata.name, time: .metadata.creationTimestamp}
            ' | jq -s 'sort_by(.time) | reverse | .[0].name // empty' | tr -d '"')
    fi

    if [ -n "$snapshot_name" ] && [ "$snapshot_name" != "null" ]; then
        debug "Found bundle snapshot: $snapshot_name"
        echo "$snapshot_name"
    else
        debug "No bundle snapshot found for SHA: $bundle_sha"
        return 1
    fi
}

# ============================================================================
# OUTPUT FUNCTIONS
# ============================================================================

output_text() {
    local output_info="$1"

    local catalog_image
    local catalog_snapshot
    local bundle_image
    local bundle_name
    local bundle_package
    local bundle_version
    local bundle_snapshot

    catalog_image=$(echo "$output_info" | jq -r '.catalog_image')
    catalog_snapshot=$(echo "$output_info" | jq -r '.catalog_snapshot')
    bundle_image=$(echo "$output_info" | jq -r '.image')
    bundle_name=$(echo "$output_info" | jq -r '.name')
    bundle_package=$(echo "$output_info" | jq -r '.package')
    bundle_version=$(echo "$output_info" | jq -r '.version')
    bundle_snapshot=$(echo "$output_info" | jq -r '.bundle_snapshot')

    info "Catalog Information:"
    if [ -n "$catalog_snapshot" ] && [ "$catalog_snapshot" != "null" ]; then
        info "  Snapshot: $catalog_snapshot"
    fi
    info "  Image: $catalog_image"
    info ""
    info "Bundle Information:"
    info "  Package: $bundle_package"
    info "  Version: $bundle_version"
    info "  Name: $bundle_name"
    if [ -n "$bundle_snapshot" ] && [ "$bundle_snapshot" != "null" ]; then
        info "  Snapshot: $bundle_snapshot"
    fi
    info "  Image: $bundle_image"
}

output_json() {
    local bundle_info="$1"
    echo "$bundle_info" | jq .
}

output_yaml() {
    local bundle_info="$1"
    echo "$bundle_info" | jq -r 'to_entries | .[] | "\(.key): \(.value)"'
}

# ============================================================================
# ARGUMENT PARSING
# ============================================================================

parse_arguments() {
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help)
                usage
                exit 0
                ;;
            -s|--snapshot)
                SNAPSHOT_NAME="$2"
                shift 2
                ;;
            -v|--version)
                VERSION="$2"
                shift 2
                ;;
            -e|--env)
                ENVIRONMENT="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -f|--format)
                OUTPUT_FORMAT="$2"
                shift 2
                ;;
            --no-cleanup)
                CLEANUP=false
                shift
                ;;
            --debug)
                DEBUG=true
                shift
                ;;
            -*)
                error "Unknown option: $1"
                usage
                exit 1
                ;;
            *)
                # Positional argument - could be catalog image or environment shorthand
                if [ -z "$CATALOG_IMAGE" ]; then
                    # Check if this is an environment name (staging/production)
                    if [ "$1" = "staging" ] || [ "$1" = "production" ]; then
                        ENVIRONMENT="$1"
                    else
                        CATALOG_IMAGE="$1"
                    fi
                else
                    error "Unexpected argument: $1"
                    usage
                    exit 1
                fi
                shift
                ;;
        esac
    done

    # Validate inputs
    local input_count=0
    [ -n "$CATALOG_IMAGE" ] && input_count=$((input_count + 1))
    [ -n "$SNAPSHOT_NAME" ] && input_count=$((input_count + 1))
    [ -n "$VERSION" ] && input_count=$((input_count + 1))

    # Special case: if only environment is provided, we'll process all versions
    if [ $input_count -eq 0 ] && [ -n "$ENVIRONMENT" ]; then
        debug "Environment-only mode: will process all versions"
        # This is valid - we'll handle it in main
    elif [ $input_count -eq 0 ]; then
        error "Must provide either CATALOG_IMAGE, --snapshot, --version, or --env"
        usage
        exit 1
    elif [ $input_count -gt 1 ]; then
        error "Cannot specify multiple input methods (catalog image, snapshot, or version)"
        usage
        exit 1
    fi

    # Validate format
    case "$OUTPUT_FORMAT" in
        text|json|yaml) ;;
        *)
            error "Invalid format: $OUTPUT_FORMAT (must be text, json, or yaml)"
            exit 1
            ;;
    esac

    debug "Configuration:"
    debug "  Namespace: ${NAMESPACE}"
    debug "  Catalog Image: ${CATALOG_IMAGE:-<from lookup>}"
    debug "  Snapshot: ${SNAPSHOT_NAME:-<none>}"
    debug "  Version: ${VERSION:-<none>}"
    debug "  Environment: ${ENVIRONMENT:-any}"
    debug "  Format: ${OUTPUT_FORMAT}"
}

# ============================================================================
# MAIN LOGIC
# ============================================================================

process_single_catalog() {
    local catalog_image="$1"
    local snapshot_name="$2"

    debug "Using catalog image: $catalog_image"

    # Extract bundle information
    local bundle_info
    bundle_info=$(extract_bundle_from_catalog "$catalog_image")

    if [ -z "$bundle_info" ]; then
        error "Failed to extract bundle information"
        return 1
    fi

    # Find the bundle snapshot
    local bundle_image
    local bundle_version
    bundle_image=$(echo "$bundle_info" | jq -r '.image')
    bundle_version=$(echo "$bundle_info" | jq -r '.version')

    local bundle_snapshot=""
    if [ -n "$bundle_image" ]; then
        bundle_snapshot=$(find_bundle_snapshot "$bundle_image" "$bundle_version" "$snapshot_name") || bundle_snapshot=""
    fi

    # Add catalog metadata and bundle snapshot to bundle info
    local output_info
    output_info=$(echo "$bundle_info" | jq -c \
        --arg catalog "$catalog_image" \
        --arg catalog_snap "$snapshot_name" \
        --arg bundle_snap "$bundle_snapshot" \
        '. + {catalog_image: $catalog, catalog_snapshot: $catalog_snap, bundle_snapshot: $bundle_snap}')

    echo "$output_info"
}

main() {
    parse_arguments "$@"
    check_dependencies
    check_auth
    setup_kubearchive

    # Check if we're in all-versions mode (only environment provided)
    if [ -z "$CATALOG_IMAGE" ] && [ -z "$SNAPSHOT_NAME" ] && [ -z "$VERSION" ] && [ -n "$ENVIRONMENT" ]; then
        debug "Processing all versions for environment: $ENVIRONMENT"

        # Known LVMS versions
        local versions=("4.17" "4.18" "4.19" "4.20" "4.21")
        local all_results=()

        for ver in "${versions[@]}"; do
            debug "Processing version $ver"

            # Get latest catalog snapshot for this version
            local snapshot_name
            snapshot_name=$(get_latest_catalog_snapshot "$ver" "$ENVIRONMENT" 2>/dev/null) || continue

            if [ -z "$snapshot_name" ]; then
                debug "No catalog snapshot found for version $ver, skipping"
                continue
            fi

            # Get catalog image from snapshot
            local catalog_image
            catalog_image=$(get_catalog_from_snapshot "$snapshot_name" 2>/dev/null) || continue

            if [ -z "$catalog_image" ]; then
                debug "Failed to get catalog image for version $ver, skipping"
                continue
            fi

            # Process this catalog
            local result
            result=$(process_single_catalog "$catalog_image" "$snapshot_name")

            if [ -n "$result" ]; then
                all_results+=("$result")
            fi
        done

        # Output all results
        if [ ${#all_results[@]} -eq 0 ]; then
            error "No catalogs found for environment: $ENVIRONMENT"
            exit 1
        fi

        case "$OUTPUT_FORMAT" in
            text)
                for result in "${all_results[@]}"; do
                    output_text "$result"
                    echo ""  # Blank line between versions
                done
                ;;
            json)
                # Combine into JSON array
                printf '%s\n' "${all_results[@]}" | jq -s '.'
                ;;
            yaml)
                for result in "${all_results[@]}"; do
                    output_yaml "$result"
                    echo "---"  # YAML document separator
                done
                ;;
        esac
        return 0
    fi

    # Single catalog mode
    # Determine the catalog image to use
    local catalog_image="$CATALOG_IMAGE"
    local snapshot_name=""

    if [ -n "$SNAPSHOT_NAME" ]; then
        debug "Looking up catalog from snapshot: $SNAPSHOT_NAME"
        snapshot_name="$SNAPSHOT_NAME"
        catalog_image=$(get_catalog_from_snapshot "$SNAPSHOT_NAME")
    elif [ -n "$VERSION" ]; then
        debug "Looking up latest catalog snapshot for version=$VERSION, env=${ENVIRONMENT:-any}"
        snapshot_name=$(get_latest_catalog_snapshot "$VERSION" "$ENVIRONMENT")
        catalog_image=$(get_catalog_from_snapshot "$snapshot_name")
    fi

    if [ -z "$catalog_image" ]; then
        error "Failed to determine catalog image"
        exit 1
    fi

    # Process single catalog
    local output_info
    output_info=$(process_single_catalog "$catalog_image" "$snapshot_name")

    if [ -z "$output_info" ]; then
        error "Failed to process catalog"
        exit 1
    fi

    # Output in requested format
    case "$OUTPUT_FORMAT" in
        text) output_text "$output_info" ;;
        json) output_json "$output_info" ;;
        yaml) output_yaml "$output_info" ;;
    esac
}

main "$@"
