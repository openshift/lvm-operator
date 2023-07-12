#!/bin/bash
set -euo pipefail

make git-sanitize

# Build and push the operator
make release-local-operator

# Run the tests
GITREV=$(git rev-parse HEAD)

export IMAGE_TAG="${GITREV}"
make deploy

make git-unsanitize