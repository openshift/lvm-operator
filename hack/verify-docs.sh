#!/bin/bash
# verify-docs.sh - Check that file paths and symbols
# referenced in docs/ still exist.
# Run from the repo root: ./hack/verify-docs.sh

# No -e: grep returns non-zero on no match, which would
# kill the script before it can report what is missing.
set -uo pipefail

ERRORS=0
DOCS_DIR="docs"
SRC_DIRS=("internal/" "api/" "cmd/")
HARNESS_DOCS=(
    "$DOCS_DIR/core-beliefs.md"
    "$DOCS_DIR/conventions/"
    "$DOCS_DIR/domain/"
)
PATH_PREFIXES="internal/|api/|cmd/|config/|hack/|test/|bundle/"

echo "=== Checking file path references in docs/ ==="

path_re='[a-zA-Z_][a-zA-Z0-9_/.-]*\.(go|yaml)'
paths=$(grep -roEh "$path_re" "$DOCS_DIR" \
    --include="*.md" 2>/dev/null \
    | grep -E "^($PATH_PREFIXES)" \
    | sort -u || true)

for p in $paths; do
    if [ ! -f "$p" ]; then
        echo "    MISSING: $p"
        ERRORS=$((ERRORS + 1))
    fi
done

if [ $ERRORS -eq 0 ]; then
    echo "    All file paths OK"
fi

echo ""
echo "=== Checking for stale line-number references ==="

line_re='[a-z_]*\.go:[0-9]'
stale=$(grep -rn "$line_re" "$DOCS_DIR" \
    --include="*.md" 2>/dev/null || true)
if [ -n "$stale" ]; then
    echo "    Found line-number references:"
    echo "$stale"
    ERRORS=$((ERRORS + 1))
else
    echo "    No line-number references found"
fi

echo ""
echo "=== Checking Go symbol references ==="

# 1) Qualified symbols (pkg.Symbol) filtered to repo packages
raw_qualified=$(grep -roh \
    '`[a-z][a-zA-Z0-9]*\.[A-Z][a-zA-Z0-9]*`' \
    "${HARNESS_DOCS[@]}" \
    --include="*.md" 2>/dev/null \
    | tr -d '`' | sort -u || true)

symbols=""
for s in $raw_qualified; do
    pkg=$(echo "$s" | cut -d. -f1)
    if grep -rql "^package $pkg$" "${SRC_DIRS[@]}" \
        --include="*.go" 2>/dev/null; then
        symbols="$symbols $s"
    fi
done

# 2) Multi-hump CamelCase identifiers (e.g. StorageClassOptions,
# DeviceDiscoveryPolicyStatic, LVMCluster, RAIDConfig).
# Pattern: 2+ uppercase letters anywhere in the identifier
# (covers both CamelCase and acronym-prefixed types).
raw_camel=$(grep -roh \
    '`[A-Z][a-zA-Z0-9]*[A-Z][a-zA-Z0-9]*`' \
    "${HARNESS_DOCS[@]}" \
    --include="*.md" 2>/dev/null \
    | tr -d '`' \
    | grep -vE '^(OK|AND|OR|NOT|IF|JSON|YAML|CR|PR|CI|SA|SC|LV|VG|PV)$' \
    | sort -u || true)

for s in $raw_camel; do
    # Skip if already covered by qualified check
    echo "$symbols" | grep -qwF "$s" && continue
    symbols="$symbols $s"
done

sym_count=0
for sym in $symbols; do
    count=$(grep -rnwF "$sym" "${SRC_DIRS[@]}" \
        --include="*.go" 2>/dev/null \
        | grep -vcF "_test.go" || true)
    if [ "$count" -eq 0 ]; then
        echo "    MISSING symbol: $sym"
        ERRORS=$((ERRORS + 1))
    fi
    sym_count=$((sym_count + 1))
done

if [ "$sym_count" -eq 0 ]; then
    echo "    WARNING: no symbols extracted"
    ERRORS=$((ERRORS + 1))
fi

echo "    Symbols checked: $sym_count"

echo ""
if [ $ERRORS -gt 0 ]; then
    echo "FAILED: $ERRORS issue(s) found"
    exit 1
else
    echo "PASSED: All doc references valid"
    exit 0
fi
