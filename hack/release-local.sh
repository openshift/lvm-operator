#!/bin/bash
set -euo pipefail

# Get the git hash for tagging the images
TEMP_COMMIT="false"
test -z "$(git status --porcelain)" || TEMP_COMMIT="true"

if [[ "${TEMP_COMMIT}" == "true" ]]; then
  git add .
  git commit -m "chore: Temporary" || true
fi

GITREV=$(git rev-parse HEAD)
BUILDER=$(command -v docker 2>&1 >/dev/null && echo docker || echo podman)

# If IMAGE_REPO is defined, build the operator image
IMAGE_REPO="${IMAGE_REPO:-}"
if [ -n "$IMAGE_REPO" ]; then
  ${BUILDER} build -t ${IMAGE_REPO}:${GITREV} .
  ${BUILDER} push ${IMAGE_REPO}:${GITREV}
fi

# If BUNDLE_REPO is defined, build the bundle
BUNDLE_REPO="${BUNDLE_REPO:-}"
if [ -n "$BUNDLE_REPO" ]; then
  BUNDLE_IMG=${BUNDLE_REPO}:${GITREV}
  ${BUILDER} build -f bundle.Dockerfile -t ${BUNDLE_IMG} .
  ${BUILDER} push ${BUNDLE_REPO}:${GITREV}

  # If CATALOG_REPO is defined, build the catalog
  CATALOG_REPO="${CATALOG_REPO:-}"
  if [ -n "$CATALOG_REPO" ]; then
    OPM="${OPM:-}"
    if [ -z "$OPM" ]; then echo "ERROR: OPM is a required variable"; exit 1; fi
    ${OPM} index add --container-tool ${BUILDER} --mode semver --tag ${CATALOG_REPO}:${GITREV} --bundles ${BUNDLE_IMG}
    ${BUILDER} push ${CATALOG_REPO}:${GITREV}
  fi
fi

echo "Built and Pushed:"
if [ -n "$IMAGE_REPO" ]; then echo "${IMAGE_REPO}:${GITREV}"; fi
if [ -n "$BUNDLE_REPO" ]; then echo "${BUNDLE_REPO}:${GITREV}"; fi
if [ -n "$CATALOG_REPO" ]; then echo "${CATALOG_REPO}:${GITREV}"; fi

# Clean up any temp commits made
if [[ "${TEMP_COMMIT}" == "true" ]]; then
  git reset --soft HEAD~1
fi