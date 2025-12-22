---
name: analyze-test-failures
argument-hint: <api-token> <reportportal-url>
description: Analyze LVMS test failures from ReportPortal
allowed-tools: WebFetch, Bash, Read, Write, Glob, Grep
---

# Analyze LVMS Test Failures

Analyzes LVMS test failures from ReportPortal by fetching launch data, extracting failed test cases, and generating a comprehensive Markdown report with failure analysis.

## Synopsis

```bash
/analyze-test-failures <api-token> <reportportal-url>
```

**Examples:**
```bash
# Using JWT token and full ReportPortal URL
/analyze-test-failures "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." https://reportportal-openshift.apps.dno.ocp-hub.prod.psi.redhat.com/ui/#prow/launches/2121

# Using Bearer token and URL with item ID
/analyze-test-failures "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." https://reportportal-openshift.apps.dno.ocp-hub.prod.psi.redhat.com/ui/#prow/launches/2121/813416/142352903
```

## Implementation

Follow these steps to analyze LVMS test failures from ReportPortal:

### Step 1: Parse Input Arguments

Parse the two required arguments:
1. **API Token** (first argument): Extract the JWT or Bearer token for authentication
2. **ReportPortal URL** (second argument): Extract the full URL to the launch or test item

From the URL, extract:
- Base domain (e.g., `reportportal-openshift.apps.dno.ocp-hub.prod.psi.redhat.com`)
- Project name (e.g., `prow`, `openshift-qe_lvms`)
- Launch ID (the first number after `/launches/`)
- Optional: Item ID (if URL points to specific test item)

**URL Pattern Examples:**
- `https://{domain}/ui/#{project}/launches/{launch_id}`
- `https://{domain}/ui/#{project}/launches/{launch_id}/{item_id}`
- `https://{domain}/ui/#{project}/launches/{launch_id}/{parent_id}/{item_id}`

### Step 2: Construct ReportPortal API Endpoints

Based on extracted information, construct the API base URL:
```
https://{domain}/api/v1/{project}
```

Construct these endpoints:
- Launch metadata: `{base_url}/launch/{launch_id}`
- Failed test items: `{base_url}/item?filter.eq.launchId={launch_id}&filter.in.status=FAILED,INTERRUPTED&page.size=300`
- Specific item (if item ID available): `{base_url}/item/{item_id}`

### Step 3: Fetch Launch Metadata

Use **Bash with curl** to retrieve launch metadata with authentication:
```bash
curl -s -H "Authorization: Bearer {api_token}" \
  "{base_url}/launch/{launch_id}"
```

Parse the JSON response to extract:
- Launch name
- Status (PASSED, FAILED, INTERRUPTED, etc.)
- Start time (convert epoch milliseconds to readable format: YYYY-MM-DD HH:MM:SS UTC)
- End time (convert epoch milliseconds to readable format)
- Duration (calculate from start/end or use provided duration)
- Attributes array: Look for key-value pairs like LVMS_version, OCP_version, profile, etc.

**Note**: If the API returns an error (401, 403, 503), check if:
- The token is valid and not expired
- The ReportPortal service is available
- The project name and launch ID are correct

### Step 4: Fetch LVMS Test Items

**IMPORTANT**: This skill automatically filters for LVMS-specific tests only. Use the ReportPortal API to retrieve tests containing "[LVMS]" in their name.

Use **Bash with curl** to retrieve LVMS test items (FAILED, INTERRUPTED, or SKIPPED):
```bash
curl -s -H "Authorization: Bearer {api_token}" \
  "{base_url}/item?filter.eq.launchId={launch_id}&filter.eq.type=STEP&filter.in.status=FAILED,INTERRUPTED,SKIPPED&filter.cnt.name=LVMS&page.size=500"
```

**Filter Parameters Explained:**
- `filter.eq.launchId={launch_id}`: Filter by launch ID
- `filter.eq.type=STEP`: Only get test steps (not parent categories)
- `filter.in.status=FAILED,INTERRUPTED,SKIPPED`: Get all LVMS tests regardless of status
- `filter.cnt.name=LVMS`: Filter for tests containing "LVMS" in the name
- `page.size=500`: Retrieve up to 500 tests

Parse the JSON response (which has a `content` array) to extract for each test:
- Test name (from `name` field)
- Status (FAILED, INTERRUPTED, or SKIPPED)
- Error/failure message (from `issue.issueType`, `issue.comment`, or error logs)
- Test item ID (from `id` field for constructing direct links)
- Any attributes like profile, LVMS_version, OCP_version from the `attributes` array
- Path names for constructing full URL path

**Handle pagination**: If response has `page.totalPages > 1`, fetch additional pages using `&page.page=2`, `&page.page=3`, etc.

**Note on SKIPPED tests**: Include skipped tests in the analysis as they indicate LVMS is not configured or available in the test environment. This is important information for understanding LVMS test coverage.

### Step 5: Construct Direct Links to Failed Tests

For each failed test item, construct the direct link using the extracted components:
```
https://{domain}/ui/#{project}/launches/{launch_id}/{item_id}
```

**Note**: Some ReportPortal instances may have additional path segments (like `/all/` or parent item IDs). Use the `pathNames` object from the API response if available to construct the exact URL path.

### Step 6: Normalize Metadata

For each failed test:
1. If test-level metadata (profile, LVMS version, OCP version) is missing, use launch-level metadata
2. If launch-level metadata is also missing, mark as "N/A"
3. Clean up test names (remove prefixes, extract readable name)
4. Truncate error messages to 150 characters if too long, append "..." if truncated

### Step 7: Generate Markdown Report

Create a comprehensive report with these sections:

#### 7.1 Launch Overview Section
```markdown
# LVMS Test Failure Analysis

## Launch Overview
- **Launch ID**: <launch_id>
- **Launch Name**: <launch_name>
- **Status**: <status>
- **Start Time**: <start_time>
- **End Time**: <end_time>
- **Duration**: <duration>
- **LVMS Version**: <lvms_version>
- **OCP Version**: <ocp_version>
- **Profile**: <profile>
- **ReportPortal Link**: <reportportal_link>
```

#### 7.2 Failed Tests Table
```markdown
## Failed Tests

| Profile | LVMS Version | OCP Version | Test Name | Failure Reason | Link |
|---------|--------------|-------------|-----------|----------------|------|
| <profile> | <lvms_version> | <ocp_version> | <test_name> | <failure_reason> | [View](<test_link>) |
```

Note: Include one row per failed test. If multiple tests share the same profile/version combination, list each test separately.

**For Skipped Tests:**
```markdown
## Skipped LVMS Tests

| Profile | OCP Version | Test Name | Status | Link |
|---------|-------------|-----------|--------|------|
| <profile> | <ocp_version> | <test_name> | SKIPPED | [View](<test_link>) |
```

Note: If ALL LVMS tests are skipped, include a note explaining that LVMS is likely not configured for this profile.

#### 7.3 Summary Section
Generate analysis including:
- Total number of failed tests
- List of affected profiles (unique values)
- Top 5 most common failure reasons (group by error message patterns)
- Categorization of failures:
  - Infrastructure failures (timeout, connection errors)
  - Test assertion failures
  - Setup/teardown failures
  - Unknown failures
- Actionable recommendations based on failure patterns

```markdown
## Summary

### Statistics
- **Total Failed Tests**: <total_count>
- **Affected Profiles**: <profile_list>
- **Unique Failure Types**: <unique_error_count>

### Top Failure Reasons
1. <error_pattern_1> (<count_1> occurrence(s))
2. <error_pattern_2> (<count_2> occurrence(s))
3. <error_pattern_3> (<count_3> occurrence(s))

### Failure Categories
- Infrastructure Failures: <infra_count>
- Test Assertion Failures: <assertion_count>
- Setup/Teardown Failures: <setup_count>
- Unknown/Other: <other_count>

## Next Steps

<analysis_recommendations>

---
*Report generated on <timestamp>*
```

### Step 8: Error Handling

Handle these error scenarios gracefully:

1. **Invalid Launch ID**: If launch ID is invalid or not found, display:
   ```
   Error: Launch ID <launch_id> not found in ReportPortal.
   Please verify the launch ID or URL is correct.
   ```

2. **API Request Failures**: If curl/API request fails, display:
   ```
   Error: Failed to fetch data from ReportPortal API.
   Error details: <error_message>
   ```

3. **No LVMS Tests Found**: If no LVMS tests are found in the launch, display:
   ```
   No LVMS tests found in launch <launch_id>.
   This may indicate LVMS tests are not included in this test profile.
   ```

4. **All LVMS Tests Skipped**: If all LVMS tests are skipped, include in the report:
   ```
   All <count> LVMS tests were skipped in this launch.
   This indicates LVMS is not configured or not available for the <profile> profile.
   ```

5. **Missing Attributes**: Mark any missing metadata as "N/A" instead of failing

6. **Partial Data**: If some data is unavailable, generate report with available data and note missing sections

### Step 9: Save Report

1. Generate a filename: `lvms-failure-report-<launch_id>-<timestamp>.md`
   - Format timestamp as: YYYYMMDD-HHMMSS
   - Example: `lvms-failure-report-2121-20251222-110000.md`
2. Use Write tool to save the report to the current directory
3. Display success message with the filename and full path
4. Optionally display a preview of the report (first 30-50 lines) to give the user a quick overview

## Example Usage

### Example 1: Analyze Prow Launch
```bash
/analyze-test-failures \
  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NjY0MzE3NjksInVzZXJfbmFtZSI6ImtuYXJyYSJ9..." \
  "https://reportportal-openshift.apps.dno.ocp-hub.prod.psi.redhat.com/ui/#prow/launches/2121"
```

### Example 2: Analyze LVMS QE Launch
```bash
/analyze-test-failures \
  "your-api-token-here" \
  "https://reportportal-openshift-qe.apps.ocp-c1.prod.psi.redhat.com/ui/#openshift-qe_lvms/launches/all/12345"
```

### Example 3: Analyze Specific Test Item
```bash
/analyze-test-failures \
  "your-api-token-here" \
  "https://reportportal-openshift.apps.dno.ocp-hub.prod.psi.redhat.com/ui/#prow/launches/2121/813416/142352903"
```

## Example Output

```markdown
# LVMS Test Failure Analysis

## Launch Overview
- **Launch ID**: <launch_id>
- **Launch Name**: <launch_name>
- **Status**: <status>
- **Start Time**: <start_time>
- **End Time**: <end_time>
- **Duration**: <duration>
- **LVMS Version**: <lvms_version>
- **OCP Version**: <ocp_version>
- **Profile**: <profile>
- **ReportPortal Link**: <reportportal_link>

## Failed Tests

| Profile | LVMS Version | OCP Version | Test Name | Failure Reason | Link |
|---------|--------------|-------------|-----------|----------------|------|
| <profile> | <lvms_version> | <ocp_version> | <test_name_1> | <failure_reason_1> | [View](<test_link_1>) |
| <profile> | <lvms_version> | <ocp_version> | <test_name_2> | <failure_reason_2> | [View](<test_link_2>) |
| <profile> | <lvms_version> | <ocp_version> | <test_name_3> | <failure_reason_3> | [View](<test_link_3>) |

## Summary

### Statistics
- **Total Failed Tests**: <total_count>
- **Affected Profiles**: <profile_list>
- **Unique Failure Types**: <unique_error_count>

### Top Failure Reasons
1. <error_pattern_1> (<count_1> occurrence(s))
2. <error_pattern_2> (<count_2> occurrence(s))
3. <error_pattern_3> (<count_3> occurrence(s))

### Failure Categories
- Infrastructure Failures: <infra_count>
- Test Assertion Failures: <assertion_count>
- Setup/Teardown Failures: <setup_count>
- Unknown/Other: <other_count>

## Next Steps

<analysis_recommendations>

---
*Report generated on <timestamp>*
```

## Notes

- **Authentication**: This skill uses JWT/Bearer tokens for ReportPortal API authentication. Tokens are typically valid for a limited time (check the `exp` claim in the JWT).
- **Getting API Token**:
  - Log in to ReportPortal UI
  - Open browser DevTools → Network tab
  - Look for API requests to see the `Authorization: Bearer <token>` header
  - Copy the token value (without "Bearer " prefix)
- **Token Expiration**: JWT tokens have an expiration time. If you get 401 errors, the token may have expired - obtain a new one.
- **Rate Limiting**: Be mindful of API rate limits when fetching large amounts of test data.
- **Data Freshness**: Launch data may take a few minutes to be fully available after test completion.
- **Large Launches**: For launches with hundreds of failures, the report may be very long. The skill handles pagination automatically.
- **Multiple ReportPortal Instances**: Different instances may have different project names (e.g., `prow`, `openshift-qe_lvms`). Ensure the URL matches the correct instance.

## Troubleshooting

**Problem**: 401 Unauthorized or 403 Forbidden errors
- **Solution**: The API token is invalid, expired, or lacks permissions. Obtain a fresh token from the ReportPortal UI.

**Problem**: 503 Service Unavailable or "Application is not available"
- **Solution**: The ReportPortal service is down or not deployed. Contact the administrator or wait for the service to come back online.

**Problem**: Launch ID not found (404 error)
- **Solution**: Verify the launch ID exists, the project name is correct, and you have access to view the launch.

**Problem**: No test items returned despite failures shown in UI
- **Solution**: The API filter may not match the UI view. Check if tests are marked as SKIPPED, INTERRUPTED, or other statuses instead of FAILED.

**Problem**: Error messages are truncated or missing
- **Solution**: Fetch detailed test logs using the item logs endpoint: `{base_url}/item/{item_id}/log`

**Problem**: URL parsing fails
- **Solution**: Ensure the URL follows the pattern `https://{domain}/ui/#{project}/launches/{launch_id}`. Some ReportPortal instances may use different URL structures.

**Problem**: Token appears in command history
- **Solution**: For security, clear your shell history after running the command, or pass the token from a secure source (environment variable, secrets manager).

## Security Considerations

- **Token Protection**: API tokens grant access to ReportPortal data. Do not share tokens or commit them to version control.
- **Token Storage**: Store tokens securely in environment variables or secrets managers.
- **Command History**: Tokens passed as command arguments may appear in shell history. Consider clearing history or using secure input methods.
- **Generated Reports**: Review generated reports before sharing - they may contain sensitive test data or environment information.
