---
name: z-stream-report
description: Generate z-stream release report for all supported LVMS versions
allowed-tools: Bash, WebFetch, Read, Write, Glob, Grep
---

# LVMS Z-Stream Release Urgency Analysis

Analyzes all currently supported LVMS versions and determines the urgency of releasing a new z-stream for each. Fetches data from Red Hat support policy, JIRA (OCPBUGS), and GitHub to produce a comprehensive urgency report with actionable recommendations.

## Prerequisites

Required environment variables:
- `JIRA_BASE_URL`: Base URL for the Jira instance (e.g., `https://issues.redhat.com`)
- `JIRA_TOKEN`: Personal Access Token for Jira authentication

## Synopsis

```bash
/z-stream-report
```

## Implementation

### Step 1: Validate Environment

Check that required environment variables are set:
```bash
echo "JIRA_BASE_URL=${JIRA_BASE_URL:-(not set)}"
echo "JIRA_TOKEN is $([ -n "$JIRA_TOKEN" ] && echo 'set' || echo 'NOT SET')"
```

If either is not set, display an error and stop:
```
Error: Required environment variables are not set.
Please set JIRA_BASE_URL and JIRA_TOKEN before running this command.

Example:
  export JIRA_BASE_URL=https://issues.redhat.com
  export JIRA_TOKEN=<your-personal-access-token>
```

### Step 2: Fetch Support Timeline

Fetch the LVMS support lifecycle data from Red Hat's official support policy page:
```
URL: https://access.redhat.com/support/policy/updates/openshift_operators
```

Use WebFetch to retrieve the page and extract the **Logical Volume Manager Storage (LVMS)** support timeline. For each version, extract:
- Version number (e.g., 4.12, 4.14, 4.16, 4.17, 4.18, 4.19, 4.20, 4.21)
- Associated OpenShift version
- GA date
- Full Support end date
- Maintenance Support end date
- EUS Term 1 end date (if applicable)
- EUS Term 2 end date (if applicable)
- EUS Term 3 end date (if applicable)

Store as:
```
SUPPORTED_VERSIONS = [
  { version: "4.XX", ocp: "4.XX", ga_date: "YYYY-MM-DD", full_support_ends: "YYYY-MM-DD", maintenance_ends: "YYYY-MM-DD", eus_term1_ends: "YYYY-MM-DD", eus_term2_ends: "YYYY-MM-DD", eus_term3_ends: "YYYY-MM-DD" },
  ...
]
```

**IMPORTANT**: A version is still supported if ANY of its support phases are active (maintenance OR any EUS term). Even-numbered minor versions (4.12, 4.14, 4.16, 4.18, 4.20) are typically EUS-eligible and may have extended support well beyond their maintenance end date. Include all versions that have at least one active support phase (end date in the future).

For determining the "support ends" date for urgency scoring, use the **latest active end date** across all support phases (maintenance, EUS Term 1/2/3).

### Step 3: Fetch Release History from Red Hat Container Catalog

For each supported version, determine the latest z-stream release and its date using `skopeo` against `registry.redhat.io`.

**3.1: List all tags from the registry:**
```bash
skopeo list-tags docker://registry.redhat.io/lvms4/lvms-operator-bundle 2>/dev/null | jq -r '.Tags[]' | sort -V
```

Filter to only clean version tags matching `v{major}.{minor}.{patch}` (exclude `-source`, build suffix tags like `-123456`, etc.).

**3.2: For each supported version (e.g., 4.18, 4.19, ...):**
1. Filter tags matching `v{version}.*` (e.g., `v4.18.0`, `v4.18.1`, `v4.18.2`) — only clean semver tags
2. Take the highest z-stream tag as the latest release
3. Get the image creation date using `skopeo inspect`:

```bash
# Use --override-arch and --override-os to handle multi-arch manifests on non-linux hosts
DATE=$(skopeo inspect --override-arch amd64 --override-os linux \
  docker://registry.redhat.io/lvms4/lvms-operator-bundle:v{tag} 2>/dev/null | \
  jq -r '.Created' | cut -d'T' -f1)
```

4. Calculate days since last release
5. Count total number of z-stream releases for this version

Store for each version:
```
latest_tag: "v4.18.4"
tag_date: "2025-10-15"
days_since_release: <calculated from today>
total_releases: 5
```

If no tags are found for a version, record "No releases found" and set days_since_release to the number of days since GA date.

**Note**: `skopeo` must be installed and the user must be authenticated to `registry.redhat.io` (via `podman login` or `skopeo login`). If `skopeo` is not available, display an error with install instructions.

### Step 4: Discover JIRA Target Version Field

Before querying bugs, discover the custom field ID for "Target Version" in this JIRA instance:

```bash
curl -s -H "Authorization: Bearer $JIRA_TOKEN" \
  -H "Content-Type: application/json" \
  "$JIRA_BASE_URL/rest/api/2/field" | \
  jq -r '.[] | select(.name | test("target.*version"; "i")) | "\(.id) \(.name)"'
```

Save the field ID (e.g., `customfield_12319940`) for use in subsequent queries. If no match is found, skip target version and rely on `fixVersions` and `versions` fields only.

### Step 5: Query JIRA for Open Bugs

Fetch all open bugs for the LVMS component. Use POST to avoid URL encoding issues.

```bash
curl -s -X POST \
  -H "Authorization: Bearer $JIRA_TOKEN" \
  -H "Content-Type: application/json" \
  "$JIRA_BASE_URL/rest/api/2/search" \
  -d '{
    "jql": "project = OCPBUGS AND component = \"Logical Volume Manager Storage\" AND type = Bug AND status not in (Closed, Verified, \"Release Pending\", ON_QA)",
    "maxResults": 500,
    "fields": ["summary", "priority", "status", "fixVersions", "versions", "labels", "created", "<TARGET_VERSION_FIELD_ID>"]
  }'
```

Replace `<TARGET_VERSION_FIELD_ID>` with the field ID discovered in Step 4. If no target version field was found, omit it from the fields list.

**Handle pagination**: If `total > maxResults`, fetch additional pages using `startAt` parameter.

**Classify each bug as CVE or regular bug:**
- **CVE**: If `labels` contains `SecurityTracking` OR `summary` matches `CVE-\d{4}-\d+`
- **Regular bug**: Everything else

**Group each bug by version:**
For each bug, determine its target version using this priority:
1. Target Version field (from Step 4) — extract the minor version (e.g., "4.18" from "4.18.z" or "4.18.0")
2. `fixVersions` — use the first entry's minor version
3. `versions` (Affects Version) — use the first entry's minor version
4. If none available, place in "Unassigned" group

**Track per version:**
- Total bug count
- CVE count
- Count by priority: Blocker, Critical, Major, Normal, Minor/Trivial

### Step 6: Calculate Urgency Score

For each supported version, calculate an urgency score (0–100) using these weighted factors:

| Factor | Weight | Scoring |
|--------|--------|---------|
| Days since last release | 25 pts max | <30d → 0, 30-60d → 10, 60-90d → 15, 90-120d → 20, >120d → 25 |
| Open CVEs | 30 pts max | 15 per Critical/Important CVE, 8 per Moderate, 3 per Low (capped at 30) |
| Blocker/Critical bugs | 20 pts max | 10 per Blocker, 5 per Critical (capped at 20) |
| Major bugs | 10 pts max | 2 per Major bug (capped at 10) |
| Support window proximity | 15 pts max | <3 months left → 15, 3-6 months → 10, 6-12 months → 5, >12 months → 0 |

**Urgency levels:**
- **CRITICAL** (75–100): Immediate z-stream release recommended
- **HIGH** (50–74): Z-stream release should be planned within 1-2 weeks
- **MEDIUM** (25–49): Z-stream release can be scheduled normally
- **LOW** (0–24): No urgent need for z-stream release

### Step 7: Generate Report

Generate and display a comprehensive markdown report with the following sections:

#### 7.1: Header and Overview Table

```markdown
# LVMS Z-Stream Release Urgency Report

**Generated**: <YYYY-MM-DD HH:MM UTC>
**Data Sources**: Red Hat Support Policy, JIRA (OCPBUGS), Red Hat Container Catalog (registry.redhat.io)

---

## Version Overview

| Version | Z-Streams | Latest Release | Days Since | Bugs | CVEs (unique) | Blockers/Crit | Support Phase | Ends | Score | Urgency |
|---------|-----------|----------------|------------|------|---------------|---------------|---------------|------|-------|---------|
| 4.XX | N (vX.Y.0 → vX.Y.Z) | vX.Y.Z (date) | NN | N | N (N) | N | <phase> | YYYY-MM-DD | NN | LEVEL |
```

Combine release history and status into a single table. Include the total z-stream count and range, the latest release with date, and the current support phase (Full Support / Maintenance / EUS Term N) with its end date.

Sort the table by urgency score descending (most urgent first).

#### 7.2: Detailed Bug Breakdown Per Version

For each version (ordered by urgency score, highest first):

```markdown
## LVMS 4.XX — Urgency: LEVEL (Score: NN/100)

**Last Release**: vX.Y.Z (NN days ago)
**Support**: <current phase> (ends YYYY-MM-DD) | <next phases if applicable>

### Score Breakdown
| Factor | Value | Points |
|--------|-------|--------|
| Days since release | NN days | X/25 |
| Open CVEs | N issues | X/30 |
| Blocker/Critical bugs | N issues | X/20 |
| Major bugs | N issues | X/10 |
| Support window | N months left | X/15 |
| **Total** | | **XX/100** |

### Open Issues
| Key | Summary | Type | Priority | Status | Age (days) |
|-----|---------|------|----------|--------|------------|
| OCPBUGS-XXXXX | CVE-XXXX-XXXXX: ... | CVE | Critical | New | NN |
| OCPBUGS-XXXXX | ... | Bug | Major | Assigned | NN |
```

Use the **Type** column to distinguish CVEs from regular bugs. Sort by: CVEs first, then by priority (Blocker > Critical > Major > Normal > Minor), then by age descending.

If a version has no open issues, display "No open issues."

#### 7.3: Unassigned Bugs

List bugs that couldn't be mapped to any supported version:

```markdown
## Bugs Without Target Version

These bugs have no target version, fix version, or affected version set. They should be triaged and assigned to a version.

| Key | Summary | Priority | Status | Age (days) |
|-----|---------|----------|--------|------------|
```

#### 7.4: Recommendation

```markdown
## Recommendation

### Most Urgent: LVMS 4.XX (Score: NN/100 — LEVEL)

**Why**: <Concise explanation of the primary urgency drivers, e.g., "2 open CVEs (1 Critical), 1 Blocker bug, and 95 days since last release.">

**Action**: <Recommended action, e.g., "Schedule immediate z-stream release. Prioritize CVE-2025-XXXXX (Critical) and OCPBUGS-XXXXX (Blocker).">

### Other Priorities

| Priority | Version | Score | Recommended Action |
|----------|---------|-------|--------------------|
| 2 | 4.XX | NN | <action> |
| 3 | 4.XX | NN | <action> |

### Observations
- <Trend or pattern, e.g., "4.19 has the most bugs but none are Critical or CVE-related">
- <Support window concern, e.g., "4.18 exits full support in 2 months — consider final z-stream">
- <Any version with 0 bugs and recent release — note as healthy>

---
*Report generated by LVMS Z-Stream Urgency Analysis*
```

### Step 8: Display Report

Display the complete report directly to the user. The overview table at the top provides quick assessment; detailed sections follow for deeper analysis.

Do NOT save the report to a file unless the user explicitly requests it.

## Error Handling

| Error | Action |
|-------|--------|
| `JIRA_BASE_URL` not set | Display setup instructions, stop |
| `JIRA_TOKEN` not set | Display setup instructions, stop |
| JIRA returns 401/403 | Display auth error, suggest checking token validity |
| JIRA returns other error | Display error, continue with available data |
| `gh` CLI not available | Warn about missing release data, skip Step 3 |
| GitHub API rate limited | Warn, continue with partial data |
| Support timeline fetch fails | Warn user, ask them to provide supported versions manually |
| No bugs found | Report 0 bugs (good news!) |
| Target Version field not found | Fall back to fixVersions/versions, note in report |

## Notes

- **JIRA Token**: Use a Personal Access Token (PAT) from your Jira instance. Generate one from your JIRA profile settings.
- **Data Freshness**: All data is fetched live; results reflect the current state at time of execution.
- **Urgency Score**: The scoring algorithm is a guideline. Use engineering judgment — a single Critical CVE may warrant an immediate release regardless of the overall score.
- **Component Name**: This skill uses `"Logical Volume Manager Storage"` as the JIRA component name. If your JIRA uses a different name, the query will return no results. Check your JIRA project's component list.
- **Version Format**: The skill handles common version formats: `4.18.z`, `4.18.0`, `4.18`, `v4.18.1`. All are normalized to the minor version (e.g., `4.18`).
- **Security Issues**: CVEs are weighted heavily (30% of score) because they often have externally imposed fix deadlines (embargo dates, public disclosure timelines).
