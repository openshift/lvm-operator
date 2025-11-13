---
description: Port an existing test from https://github.com/openshift/openshift-tests-private to the local repository
argument-hint: [test-package] [test-case]
---

## Name
openshift:port-otp-test

## Synopsis
```
/port-otp-test [test-package] [test-case]
```

## Description

The `port-otp-test` command assists in porting and validating
existing tests from the Openshift tests private repo into the local reposity.
It follows best practices for Ginkgo-based testing and ensures test reliability through automated
validation.

This command handles the complete porting process:
- Porting a test case from https://github.com/openshift/openshift-tests-private into the local repository
- Imports the https://github.com/openshift-eng/openshift-tests-extension framework to allow external repositories to contribute tests to openshift-tests' suites with extension binaries
- Validates tests for reliability through multiple test runs
- Ensures proper test naming and structure

## Test Framework Guidelines

### Ginkgo Framework
- OpenShift-tests and extension binaries uses **Ginkgo** as their testing framework
- Tests are organized in a BDD (Behavior-Driven Development) style with Describe/Context/It blocks
- All tests should follow Ginkgo patterns and conventions except
    - You MUST NOT use BeforeAll, AfterAll hooks
    - MUST NOT use ginkgo.Serial, instead use the [Serial] annotation in the test name if non-parallel execution is required

### Repository-Specific Guidelines

#### lvm-operator Repository Tests

If working in the "lvm-operator" code repository:
- All tests should go into the `test/integration` directory
- Utility and helper functions go in test/integration/lvms_utils.go
- You MUST NOT name the file with the _test.go suffix, or they will be ignored by “go build” and won’t be compiled, hence they won’t be part of extension binary
- If creating a new package, import it into `test/integration/integration.go`
- Always keep the original name of the ported test
- After adding a test, **MUST** rebuild the integration-test binary using `make integration-build`
- Verify that the ported tests are listed by using `integration-test list`

## Implementation

The command performs the following steps:

1. **Analyze Existing Package**: Parse the test package file provided by the user
2. **Port Test**: Create a new test package following the openshift-tests-extension framework
   - Determine correct location
   - Adjust the code to use the openshift-tests-extension framework
3. **Build Binary**: Rebuild the integration-test binary
4. **Check Case List**: List the test cases using `integration-test list`

## Arguments

- **$1** (test-package): Path to the openshift-tests-private .go package file
- **$2** (test-case): The name of the specific case to port
