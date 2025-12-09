# Konflux CLI Login
This command helps you log into the Konflux CLI for working with the LVMS operator in the Konflux build system.

## Purpose
Authenticate with Konflux to manage application builds, components, and integration tests for the LVMS operator.

## Prerequisites
- Openshift CLI (`oc`) must be installed
- Valid Konflux/OpenShift credentials
- Access to the LVMS project in the Konflux cluster

## Steps

### 1. Verify Konflux CLI Installation
Check that the OC CLI is available:
**Commands:**
```bash
# Check if konflux CLI is installed
which oc
oc version
```

**Expected Output:**
```
*/oc
Client Version: x.y.z
Kustomize Version: vx.y.z
```

### 2. Login to Konflux
Authenticate with your credentials:

**Commands:**
```bash
# Login to the Konflux cluster
oc login --web https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443/
```

**Expected Output:**
```
Opening browser for authentication...
Successfully logged in to Konflux
```

### 3. Verify Authentication
Confirm you're logged in and have access to the LVMS namespace:

**Commands:**
```bash
# Check current context
oc auth whoami

# List available namespaces
oc projects
```

**Expected Output:**
- The `oc auth whoami` command should return without an error
- the `oc projects` command should include "logical-volume-manag-tenant" in the list of projects the user has access to

## Notes
- Konflux uses OAuth tokens that expire after a period of inactivity
- Re-authentication may be required for long-running sessions
- Ensure you're working with the correct application context when managing LVMS components
