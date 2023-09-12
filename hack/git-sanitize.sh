#!/bin/bash
set -euo pipefail

TEMP_COMMIT_MSG="discard: Temporary"

CLEANUP="${CLEANUP:-}"
if [ -z "${CLEANUP}" ]; then
    # Do a temp commit if the working dir is dirty
    TEMP_COMMIT="false"
    test -z "$(git status --porcelain)" || TEMP_COMMIT="true"

    if [[ "${TEMP_COMMIT}" == "true" ]]; then
        echo "Creating temp commit"
        git add .
        git commit -m "${TEMP_COMMIT_MSG}" || true
    fi
else
    # Cleanup a temp commit if one was needed
    PREV_MSG=$(git log -1 --pretty=%B)

    if [[ $PREV_MSG == *"${TEMP_COMMIT_MSG}"* ]]; then
        echo "Cleaning up temp commit"
        git reset --soft HEAD~1
    fi
fi
