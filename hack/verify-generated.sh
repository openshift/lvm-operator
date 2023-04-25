#!/bin/bash
set -euo pipefail

function print_failure {
  echo "There are unexpected changes to the tree when running 'make generate' and 'make manifests'. Please"
  echo "run these commands locally and double-check the Git repository for unexpected changes which may"
  echo "need to be committed."
  exit 1
}

if [ "${OPENSHIFT_CI:-false}" = true ]; then
  make generate
  make manifests

  test -z "$(git status --porcelain | \grep -v '^??')" || print_failure
  echo "verified generated manifests and deep copy"
fi
