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

### Import Style and Aliases
All ported tests MUST use the following import pattern with aliases:

```go
import (
    // Standard library imports first
    "context"
    "fmt"
    "time"
    // ... other standard library imports

    // Ginkgo/Gomega with standard aliases
    g "github.com/onsi/ginkgo/v2"
    o "github.com/onsi/gomega"

    // OpenShift test utilities with standard aliases
    exutil "github.com/openshift/origin/test/extended/util"
    compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"

    // Kubernetes imports
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    // ... other k8s imports

    e2e "k8s.io/kubernetes/test/e2e/framework"
)
```

**Critical Import Rules:**
- ALWAYS use `g` as the alias for `github.com/onsi/ginkgo/v2`
- ALWAYS use `o` as the alias for `github.com/onsi/gomega`
- ALWAYS use `exutil` as the alias for `github.com/openshift/origin/test/extended/util`
- ALWAYS use `compat_otp` as the alias for `github.com/openshift/origin/test/extended/util/compat_otp`
- Use Ginkgo functions with the `g.` prefix (e.g., `g.Describe`, `g.It`, `g.By`)
- Use Gomega matchers with the `o.` prefix (e.g., `o.Expect`, `o.BeNil`)

### Cluster Client Initialization
**ALWAYS** use `exutil.NewCLI()` for cluster connections:

```go
var (
    oc = exutil.NewCLI("test-namespace-prefix")
)
```

- NEVER use `kubernetes.Clientset` directly
- NEVER use `clientcmd.BuildConfigFromFlags`
- Use `oc.AdminKubeClient()` to access the Kubernetes clientset
- Use `oc.AdminConfig()` to access the rest config
- Example: `oc.AdminKubeClient().CoreV1().Pods(namespace).List(...)`

### Repository-Specific Guidelines

#### lvm-operator Repository Tests

If working in the "lvm-operator" code repository:
- Integration test cases exist in `test/integration/tests/lvms.go`
- Utility and helper functions go in `test/integration/tests/lvms_utils.go`
- You MUST NOT name the file with the `_test.go` suffix, or they will be ignored by "go build" and won't be compiled, hence they won't be part of extension binary
- Test files are in the `tests` package which is already imported in `test/integration/integration.go`
- Always keep the original name of the ported test
- MUST NOT remove the [Disruptive] tag from test case names
- After adding a test, **MUST** rebuild the integration-test binary:
  ```bash
  cd test/integration && make integration-build
  ```
- Verify that the ported tests are listed:
  ```bash
  ./integration-test list | grep "test-case-name"
  ```

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
   - Create/update test file in `test/integration/tests/lvms.go`
   - Migrate utilities to `test/integration/tests/lvms_utils.go`
   - Convert imports to use standard aliases (g, o, exutil, compat_otp)
   - Replace all Ginkgo calls with `g.` prefix (Describe → g.Describe, It → g.It, By → g.By)
   - Replace all Gomega calls with `o.` prefix (Expect → o.Expect, BeNil → o.BeNil)
   - Replace `clientset` usage with `oc.AdminKubeClient()`
   - Initialize cluster client using `oc = exutil.NewCLI("namespace-prefix")`
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

When porting from openshift-tests-private, apply these conversions:

**Import Conversions:**
```go
// OLD (openshift-tests-private):
import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
)

// NEW (lvm-operator integration tests):
import (
    g "github.com/onsi/ginkgo/v2"
    o "github.com/onsi/gomega"
    exutil "github.com/openshift/origin/test/extended/util"
    compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)
```

**Client Initialization Conversions:**
```go
// OLD:
var oc = compat_otp.NewCLI("test-prefix")

// NEW:
var oc = exutil.NewCLI("test-prefix")
// Then use: oc.AdminKubeClient().CoreV1()...
```

**Additional Notes:**
- Check for existing helper functions in `test/integration/tests/lvms_utils.go` before porting
- Some openshift-tests-private helpers may not be needed with compat_otp utilities
- Replace direct REST client usage with `oc.AdminConfig()` when needed

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
- Test case exists in `test/integration/tests/lvms.go`
- All imports use standard aliases (g, o, exutil, compat_otp)
- Cluster client uses `oc = exutil.NewCLI()` pattern
- All clientset references replaced with `oc.AdminKubeClient()`
- All Ginkgo/Gomega calls use appropriate prefixes (g., o.)
- Test appears in `integration-test list` output
- Test runs successfully (if cluster available)
- Test shows no flakiness over multiple runs
- Code follows Ginkgo best practices for this repository
