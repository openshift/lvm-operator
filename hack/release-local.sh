#!/bin/bash
set -euo pipefail

# Do a temp commit if the working dir is dirty
DIRTY_REPO="false"
test -z "$(git status --porcelain)" || DIRTY_REPO="true"
if [[ "${DIRTY_REPO}" == "true" ]]; then
    echo "Dirty repository detected. Please run 'make git-sanitize' or commit your changes before running this command"
    exit 1
fi

GITREV=$(git rev-parse HEAD)
BUILDER=$(command -v docker 2>&1 >/dev/null && echo docker || echo podman)

# Run the generate and bundle commands
export IMAGE_TAG="${GITREV}"
make generate bundle

# If IMAGE_REPO is defined, build the operator image
IMAGE_REPO="${IMAGE_REPO:-}"
if [ -n "$IMAGE_REPO" ]; then
    ${BUILDER} build -t ${IMAGE_REPO}:${GITREV} .
    ${BUILDER} push ${IMAGE_REPO}:${GITREV}
fi

# If BUNDLE_REPO is defined, build the bundle
BUNDLE_REPO="${BUNDLE_REPO:-}"
CATALOG_REPO="${CATALOG_REPO:-}"
if [ -n "$BUNDLE_REPO" ]; then
    BUNDLE_IMG=${BUNDLE_REPO}:${GITREV}
    ${BUILDER} build -f bundle.Dockerfile -t ${BUNDLE_IMG} .
    ${BUILDER} push ${BUNDLE_REPO}:${GITREV}

    # If CATALOG_REPO is defined, build the catalog
    if [ -n "$CATALOG_REPO" ]; then
        OPM="${OPM:-}"
        if [ -z "$OPM" ]; then
            echo "ERROR: OPM is a required variable"
            exit 1
        fi
        ${OPM} index add --container-tool ${BUILDER} --mode semver --tag ${CATALOG_REPO}:${GITREV} --bundles ${BUNDLE_IMG}
        ${BUILDER} push ${CATALOG_REPO}:${GITREV}
    fi
fi

echo
echo "Built and Pushed:"

if [ -n "$IMAGE_REPO" ]; then echo "${IMAGE_REPO}:${GITREV}"; fi

if [ -n "$BUNDLE_REPO" ]; then echo "${BUNDLE_REPO}:${GITREV}"; fi

if [ -n "$CATALOG_REPO" ]; then echo "${CATALOG_REPO}:${GITREV}"; fi
