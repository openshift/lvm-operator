#!/bin/bash
set -euo pipefail

function print_failure {
  echo "commitlint failed"
  exit 1
}

echo "installing commitlint cli"
npm config set fund false
npm install --yes commitlint@latest conventional-changelog-conventionalcommits --quiet

npx commitlint --from HEAD~1 --to HEAD --verbose

rm -r node_modules package.json package-lock.json