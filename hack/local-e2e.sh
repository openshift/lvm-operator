#!/bin/bash
set -euo pipefail

# Sanitize the local repo (there are probably changes if this is being run locally)
make git-sanitize

# Make sure the operater, bundle, and catalog are fresh
make release-local-catalog

# Run the tests
GITREV=$(git rev-parse HEAD)

export IMAGE_TAG="${GITREV}"
make e2e-test

# Cleanup any sanitization work
make git-unsanitize