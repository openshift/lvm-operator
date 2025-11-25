---
description: Port an existing test from https://github.com/openshift/openshift-tests-private to the local repository
argument-hint: (test-package) (test-case)
---

## Name
openshift:port-otp-test

## Synopsis
```
/port-otp-test (test-package) (test-case)
```

## Arguments

- **$1** (test-package): Relative path from openshift-tests-private repository root to the test file
  - Example: `test/extended/storage/lvms.go`

- **$2** (test-case): The exact name of the test case as it appears in the It() block
  - Example: `"Author:rdeore-Critical-61586-[LVMS] [Block] Clone a pvc with Block VolumeMode"`
  - Must match the string in It("...") exactly

## Description

The `port-otp-test` command assists in porting and validating
existing tests from the OpenShift tests private repo into the local repository.
It follows best practices for Ginkgo-based testing and ensures test reliability through automated
validation.

This command handles the complete porting process:
- Porting a test case from https://github.com/openshift/openshift-tests-private into the local repository
- Imports the https://github.com/openshift-eng/openshift-tests-extension framework to allow external repositories to contribute tests to openshift-tests' suites with extension binaries
- Validates tests for reliability through multiple test runs
- Ensures proper test naming and structure

## Prerequisites

- Access to https://github.com/openshift/openshift-tests-private repository
- Understanding of the test case being ported
- Knowledge of openshift-tests-extension framework basics
- Local cluster access (optional, for validation)

## Test Framework Guidelines

### Ginkgo Framework
- OpenShift-tests and extension binaries uses **Ginkgo** as their testing framework
- Tests are organized in a BDD (Behavior-Driven Development) style with Describe/Context/It blocks
- All tests should follow Ginkgo patterns and conventions except
    - MUST NOT use BeforeAll, AfterAll hooks
    - MUST NOT use ginkgo.Serial, instead use the [Serial] annotation in the test name if non-parallel execution is required

### Repository-Specific Guidelines

#### lvm-operator Repository Tests

If working in the "lvm-operator" code repository:
- Integration test cases exist in `test/integration/lvms.go`
- Utility and helper functions go in `test/integration/lvms_utils.go`
- You MUST NOT name the file with the `_test.go` suffix, or they will be ignored by "go build" and won't be compiled, hence they won't be part of extension binary
- If creating a new package, import it into `test/integration/integration.go`
- Always keep the original name of the ported test
- MUST NOT remove the [Disruptive] tag from test case names
- After adding a test, **MUST** rebuild the integration-test binary using `make integration-build`
- Verify that the ported tests are listed by using `integration-test list`

## Examples

### Port a specific LVMS test with full test name
```bash
/port-otp-test test/extended/storage/lvms.go "Author:rdeore-Critical-61586-[LVMS] [Block] Clone a pvc with Block VolumeMode"
```

### Port another LVMS test
```bash
/port-otp-test test/extended/storage/lvms.go "should create volume group with device paths"
```

## Implementation

The command performs the following steps:

1. **Locate Source Test**:
   - Clone/fetch from https://github.com/openshift/openshift-tests-private
   - Find the test package and specific test case
   - Identify dependencies and imports

2. **Analyze Test Structure**:
   - Extract test case logic from Describe/It blocks
   - Identify required imports and utilities
   - Note any test fixtures or data files needed

3. **Port Test Code**:
   - Create/update test file in `test/integration/lvms.go`
   - Migrate utilities to `test/integration/lvms_utils.go`
   - Convert imports to use openshift-tests-extension framework
   - Maintain original test name for traceability

4. **Validate Test Structure**:
   - Ensure no BeforeAll/AfterAll hooks
   - Check for [Serial] annotations if needed
   - Verify Ginkgo patterns are followed

5. **Build and Verify**:
   - Run `make integration-build`
   - Verify test appears in `integration-test list`
   - Run the specific test case to validate functionality
   - Run 3-5 times to check for flakiness

## Validation Steps

After porting, validate the test:

1. **List Test**: Verify test appears in output
   ```bash
   ./integration-test list | grep "test-case-name"
   ```

2. **Run Test** (if cluster available):
   ```bash
   ./integration-test run "test-case-name"
   ```

3. **Check for Flakiness**: Run multiple times
   ```bash
   for i in {1..5}; do ./integration-test run "test-case-name"; done
   ```

## Important Notes

### Handling Dependencies
- Port any shared utilities the test depends on
- Update import paths to use local implementations
- Verify test data files are copied if needed

### Framework Differences
- openshift-tests-private may use different helpers
- Check for existing helper functions in test/integration/lvms_utils.go
- Adapt authentication/client initialization to extension framework
- Convert cluster configuration access patterns

### Maintaining Traceability
- Keep original test name unchanged
- Add comment with source file location
- Link to original test in openshift-tests-private

## Troubleshooting

### Test not listed after build
- Verify file doesn't have `_test.go` suffix
- Check package is imported in `test/integration/integration.go`
- Review build errors from `make integration-build`

### Import errors
- Use openshift-tests-extension framework imports
- Update module dependencies if needed
- Check for deprecated APIs

### Test fails after porting
- Verify cluster prerequisites match test requirements
- Check for hardcoded assumptions about environment
- Review timeout values for longer-running operations

## Expected Outcome

After successful porting:
- Test case exists in `test/integration/lvms.go`
- Test appears in `integration-test list` output
- Test runs successfully (if cluster available)
- Test shows no flakiness over multiple runs
- Code follows Ginkgo best practices for this repository
