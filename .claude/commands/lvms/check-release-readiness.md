---
name: check-release-readiness
argument-hint: [--version <release-version>] [--k8s <kubernetes-version>] [--local] [--branch <branch>]
description: Check LVMS release readiness by verifying branches, dependencies, and configuration
allowed-tools: Bash, Read, Glob, Grep, WebFetch
---

# LVMS Release Readiness Check

This command performs a comprehensive release readiness check for LVMS (Logical Volume Manager Storage). It verifies that all prerequisites are in place for a successful release, including release branches, dependency versions, and configuration updates.

**IMPORTANT**: By default, this command checks the **upstream** `github.com/openshift/lvm-operator` repository (main branch) to ensure accurate release readiness assessment. Use `--local` to check the local working directory instead.

## Synopsis

```bash
/check-release-readiness [--version <release-version>] [--k8s <kubernetes-version>] [--local] [--branch <branch>]
```

**Examples:**
```bash
# Check upstream main branch (default) - will prompt for required information
/check-release-readiness

# Specify release version only (will prompt for Kubernetes version)
/check-release-readiness --version 4.21

# Specify both release and Kubernetes versions
/check-release-readiness --version 4.21 --k8s 1.34

# Short form
/check-release-readiness 4.21 1.34

# Check a specific upstream branch (e.g., release branch)
/check-release-readiness --version 4.21 --k8s 1.34 --branch release-4.21

# Check local working directory instead of upstream
/check-release-readiness --version 4.21 --k8s 1.34 --local
```

## Implementation

### Step 1: Parse Arguments and Gather Information

Parse the provided arguments:
- `--version <version>` or first positional argument: Release version
- `--k8s <version>` or second positional argument: Kubernetes version
- `--local`: Check local working directory instead of upstream
- `--branch <branch>`: Check a specific upstream branch (default: `main`, ignored if --local)

Set internal variables:
```
RELEASE_VERSION = from --version or first positional arg
K8S_VERSION = from --k8s or second positional arg
K8S_MINOR = minor version extracted from K8S_VERSION (e.g., 34 from 1.34)
USE_LOCAL = true if --local, else false
UPSTREAM_BRANCH = from --branch or "main"
UPSTREAM_BASE_URL = https://raw.githubusercontent.com/openshift/lvm-operator/${UPSTREAM_BRANCH}
```

**If Kubernetes version is not provided**, ask the user:
```
What is the target Kubernetes version for this release?
(This is usually communicated via the release planning email)
Examples: 1.32, 1.33, 1.34
```

### File Access Pattern

Throughout this skill, use these patterns to read repository files:

| Mode | Pattern |
|------|---------|
| **Upstream** (default) | `curl -s "${UPSTREAM_BASE_URL}/{file}"` |
| **Local** (`--local`) | Read `{file}` directly or use `grep` |

Examples:
- go.mod: `curl -s "${UPSTREAM_BASE_URL}/go.mod"` or `cat go.mod`
- Makefile: `curl -s "${UPSTREAM_BASE_URL}/Makefile"` or `cat Makefile`

### Step 2: Initialize Checklist

Create an internal checklist to track:
```
- [ ] Release branch exists: openshift/lvm-operator (release-X.Y)
- [ ] Release branch exists: openshift/topolvm (release-X.Y)
- [ ] Go version matches Kubernetes go version
- [ ] Kubernetes dependencies (k8s.io/*) updated to v0.{k8s_minor}.x
- [ ] CSI dependencies use correct k8s.io/* version
- [ ] TopoLVM replacement points to openshift/topolvm
- [ ] Upstream TopoLVM sync is recent (not stale)
- [ ] Controller-runtime version is compatible
- [ ] Makefile tool versions updated (ENVTEST_K8S_VERSION, OPERATOR_SDK_VERSION)
- [ ] release/operator/rpms.lock.yaml exists
```

### Step 3: Check Release Branches

Verify release branches exist on both repositories:

**LVM Operator Branch:**
```bash
gh api repos/openshift/lvm-operator/branches/release-{release_version} --jq '.name' 2>/dev/null || echo "NOT_FOUND"
```

**TopoLVM Branch:**
```bash
gh api repos/openshift/topolvm/branches/release-{release_version} --jq '.name' 2>/dev/null || echo "NOT_FOUND"
```

Record status for each:
- If branch exists: Mark as DONE
- If branch doesn't exist: Mark as PENDING with note "Branch needs to be created"

### Step 4: Check Go Version

The Go version should match what Kubernetes uses.

1. Fetch expected Go version from Kubernetes:
```bash
curl -s "https://raw.githubusercontent.com/kubernetes/kubernetes/v${K8S_VERSION}.0/go.mod" | grep "^go " | awk '{print $2}'
```

2. Read go.mod (using File Access Pattern) and extract: `grep "^go " | awk '{print $2}'`

3. Compare minor versions (e.g., `1.24` from `1.24.11`):
   - Match: Mark as DONE
   - Mismatch: Mark as PENDING

### Step 5: Check Kubernetes Dependencies

The k8s.io/* dependencies should be at version `v0.{K8S_MINOR}.x`.

**Dependencies to check:**
- k8s.io/api, apiextensions-apiserver, apimachinery, apiserver
- k8s.io/client-go, component-helpers, csi-translation-lib, kubelet

**Excluded** (different versioning): `k8s.io/klog/v2`, `k8s.io/utils`

Read go.mod and grep for:
```bash
grep -E "k8s.io/(api|apiextensions-apiserver|apimachinery|apiserver|client-go|component-helpers|csi-translation-lib|kubelet) "
```

- If all match `v0.{K8S_MINOR}.x`: Mark as DONE
- If any are older: Mark as PENDING with current and expected versions

### Step 6: Check CSI Dependencies

CSI dependencies must use `k8s.io/* v0.{K8S_MINOR}.x`.

**CSI dependencies to verify:**
- kubernetes-csi/csi-lib-utils
- kubernetes-csi/external-provisioner
- kubernetes-csi/external-resizer
- kubernetes-csi/external-snapshotter

1. Read go.mod and extract current CSI versions: `grep "kubernetes-csi"`

2. For each CSI dependency, check its go.mod to verify k8s.io/client-go version:
```bash
curl -s "https://raw.githubusercontent.com/kubernetes-csi/{repo}/{version}/go.mod" | grep "k8s.io/client-go"
```

3. If any use older k8s.io versions, find compatible versions:
```bash
gh api repos/kubernetes-csi/{repo}/tags --jq '.[0:10] | .[].name'
```

- DONE: All CSI deps use k8s.io/* `v0.{K8S_MINOR}.x`
- PENDING: Include which versions need updating and recommend replacements

### Step 7: Check TopoLVM Replacement

Read go.mod and extract TopoLVM replacement: `grep "replace.*topolvm"`

The replacement should point to `github.com/openshift/topolvm` with a pseudo-version format:
```
v0.X.Y-0.YYYYMMDDHHMMSS-COMMITSHA
```

Save the extracted version for use in Step 8.

- DONE: Replacement points to openshift/topolvm
- PENDING: Replacement missing or points elsewhere

### Step 8: Check Upstream TopoLVM Sync Status

**IMPORTANT**: Verify that the openshift/topolvm fork is not too far behind the upstream topolvm/topolvm repository. Stale forks can miss important bug fixes, security patches, and new features.

#### 8.1: Get Sync Date from TopoLVM Replacement

Use the TopoLVM replacement extracted in Step 7. The pseudo-version format is:
```
v0.X.Y-0.YYYYMMDDHHMMSS-COMMITSHA
```

Parse the date (YYYYMMDD) from the pseudo-version to determine when the last sync occurred.

#### 8.2: Fetch Upstream TopoLVM Releases

Get recent releases from upstream topolvm/topolvm to compare:
```bash
gh api repos/topolvm/topolvm/releases --jq '.[0:5] | .[] | "\(.tag_name) \(.published_at)"' 2>/dev/null
```

#### 8.3: Determine Sync Staleness

Calculate staleness by comparing:
1. Days since last sync (from pseudo-version date)
2. Number of upstream releases since last sync

**Staleness Thresholds**:
- **OK** (< 30 days): Fork is reasonably current
- **WARNING** (30-90 days): Fork may be missing recent updates
- **STALE** (> 90 days): Fork is significantly behind upstream, sync recommended before release

Also consider upstream releases:
- If 1+ minor version releases missed: Mark as WARNING
- If 2+ minor version releases missed: Mark as STALE

#### 8.4: Record Upstream Sync Status

Mark as:
- **DONE**: Last sync within 30 days AND no major upstream releases missed
- **WARNING**: Last sync 30-90 days ago OR 1 minor upstream release missed
- **STALE**: Last sync > 90 days ago OR 2+ upstream releases missed

Include in the report:
- Date of last upstream sync (from go.mod pseudo-version)
- Days since last sync
- Latest upstream release version and date
- Number of upstream releases since last sync

### Step 9: Check Makefile Tool Versions

Read Makefile and extract: `grep -E "(ENVTEST_K8S_VERSION|OPERATOR_SDK_VERSION)"`

- **ENVTEST_K8S_VERSION**: Should be `{K8S_VERSION}.x` or close
- **OPERATOR_SDK_VERSION**: Should be recent stable version

Mark as DONE if appropriate, PENDING if updates needed.

### Step 10: Check rpms.lock.yaml

Check if `release/operator/rpms.lock.yaml` exists (using File Access Pattern).

- DONE: File exists (note: regenerate with `make rpm-lock` after dependency updates)
- PENDING: File missing

### Step 11: Check Controller-Runtime Version

Read go.mod and extract: `grep "sigs.k8s.io/controller-runtime"`

**Version formula**: controller-runtime `v0.{K8S_MINOR - 12}.x` or later.
- Example: For Kubernetes 1.34 → controller-runtime v0.22.x+

- DONE: Version matches or exceeds expected
- PENDING: Version too old

### Step 12: Generate Release Readiness Report

Generate a comprehensive report in markdown format:

```markdown
# LVMS Release Readiness Report

**Release Version**: {RELEASE_VERSION}
**Target Kubernetes Version**: {K8S_VERSION}
**Report Generated**: {timestamp}
**Source**: {upstream URL or "Local working directory"}

---

## Summary

| Category | Status | Details |
|----------|--------|---------|
| Release Branches | {status_icon} | {summary} |
| Go Version | {status_icon} | {summary} |
| Kubernetes Dependencies | {status_icon} | {summary} |
| CSI Dependencies | {status_icon} | {summary} |
| TopoLVM Replacement | {status_icon} | {summary} |
| Upstream TopoLVM Sync | {status_icon} | {summary} |
| Controller-Runtime | {status_icon} | {summary} |
| Makefile Tooling | {status_icon} | {summary} |

**Overall Status**: {READY_FOR_RELEASE | PENDING_ITEMS | NOT_READY}

---

## Detailed Checklist

### Release Branches

| Repository | Branch | Status |
|------------|--------|--------|
| openshift/lvm-operator | release-{version} | {status_with_icon} |
| openshift/topolvm | release-{version} | {status_with_icon} |

### Go Version

- **Current**: {current_go_version}
- **Expected**: {expected_go_version} (based on Kubernetes {k8s_version})
- **Status**: {status_with_icon}

### Kubernetes Dependencies (k8s.io/*)

Expected version pattern: `v0.{k8s_minor}.x`

| Dependency | Current Version | Expected | Status |
|------------|-----------------|----------|--------|
| k8s.io/api | {version} | v0.{minor}.x | {status_icon} |
| k8s.io/apimachinery | {version} | v0.{minor}.x | {status_icon} |
| k8s.io/client-go | {version} | v0.{minor}.x | {status_icon} |
| ... | ... | ... | ... |

### CSI Dependencies

CSI dependencies must use k8s.io/* versions matching the target Kubernetes version (v0.{k8s_minor}.x).

| Dependency | Current Version | Uses k8s.io/* | Expected | Status |
|------------|-----------------|---------------|----------|--------|
| kubernetes-csi/csi-lib-utils | {version} | {k8s_version_used} | v0.{minor}.x | {status_icon} |
| kubernetes-csi/external-provisioner | {version} | {k8s_version_used} | v0.{minor}.x | {status_icon} |
| kubernetes-csi/external-resizer | {version} | {k8s_version_used} | v0.{minor}.x | {status_icon} |
| kubernetes-csi/external-snapshotter | {version} | {k8s_version_used} | v0.{minor}.x | {status_icon} |

{If any CSI dependency needs update, include:}
**Action Required**: Update CSI dependencies to versions that use k8s.io/* v0.{k8s_minor}.x

### TopoLVM Replacement

- **Current Replacement**: {topolvm_replacement}
- **Status**: {status_with_icon}
- **Note**: {note_about_branch_alignment}

### Upstream TopoLVM Sync Status

| Metric | Value |
|--------|-------|
| Last Sync Date | {sync_date} |
| Days Since Sync | {days_since_sync} |
| Latest Upstream Release | {latest_upstream_version} ({upstream_release_date}) |
| Upstream Releases Since Sync | {releases_since_sync} |
| Status | {status_with_icon} |

{If STALE or WARNING, include:}
**Missed Upstream Releases:**
| Version | Release Date | Notable Changes |
|---------|--------------|-----------------|
| {version} | {date} | {summary_of_changes} |

**Recommendation**: {recommendation_based_on_staleness}

### Tooling Versions (Makefile)

| Tool | Current Version | Expected | Status |
|------|-----------------|----------|--------|
| ENVTEST_K8S_VERSION | {version} | {expected} | {status_icon} |
| OPERATOR_SDK_VERSION | {version} | Latest stable | {status_icon} |

### Controller-Runtime (go.mod)

| Dependency | Current Version | Expected | Status |
|------------|-----------------|----------|--------|
| sigs.k8s.io/controller-runtime | {version} | v0.{expected_minor}.x | {status_icon} |

### Configuration Files

| File | Status | Notes |
|------|--------|-------|
| release/operator/rpms.lock.yaml | {status_icon} | Regenerate with `make rpm-lock` after dependency updates |

---

## Action Items

{List of items that need attention, numbered and with clear instructions}

1. {action_item_1}
2. {action_item_2}
...

## Next Steps

{Based on the status, provide recommended next steps}

If all items are DONE:
- The release branch is ready for testing
- Proceed with building and publishing release artifacts

If items are PENDING:
- Complete the pending items listed above
- Re-run this check after updates: `/check-release-readiness --version {version} --k8s {k8s_version}`
- Reference docs/dependency-management.md for update procedures

---

*Generated by LVMS Release Readiness Check*
```

### Status Icons

Use these status indicators:
- DONE/PASS: `[x]` or checkmark
- PENDING/NEEDS_UPDATE: `[ ]` or warning
- FAILED/MISSING: `[!]` or error
- N/A: `[-]` or dash
- NEEDS_REVIEW: `[?]` or question

### Step 13: Display Report

Display the generated report directly to the user. The report should be comprehensive but scannable, with the summary table at the top for quick assessment.

## Error Handling

| Error | Action |
|-------|--------|
| `gh` command fails | Fall back to `git ls-remote` for branch checks |
| Network errors (upstream) | Warn user, suggest `--local` flag |
| go.mod not found (local) | Error: run from repository root or omit `--local` |

## Notes

- Run before creating release branch or tagging a release
- After updates: `make godeps-update` then `make docker-build`
- Reference: `docs/dependency-management.md`
