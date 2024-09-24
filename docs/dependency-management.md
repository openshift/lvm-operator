# Dependency Management and Updating of LVM Operator

LVM Operator as a CSI Driver + Operator Meta-Operator uses multiple dependencies to function properly. These dependencies are managed using the `go mod` tool and are in vendored format.

Due to being a CSI driver and as a packaged OLM operator, every minor Kubernetes release warrants a revisit and update of all dependencies. This is to ensure that the operator is compatible with the latest Kubernetes release as well as to preempt any breaking changes that may have been introduced in the dependencies. As such it is imperative to keep the dependencies not only up-to-date but also in sync with the Kubernetes release.

## Updating Dependencies

To update the dependencies, follow the steps below:

1. Update the go.mod file with the new version of go corresponding to the go version used by kubernetes itself. An example for 1.31 is https://github.com/kubernetes/kubernetes/blob/v1.31.1/go.mod#L9.
    Note that we only use the latest minor for pinning in the Dockerfile, and use the latest Z-stream in our go.mod file at the time of the update.
    Do not jump into the next minor Go version if available but unused in upstream.
    Also note that the base image used in the pipeline may restrict the maximum z-stream you can use.
2. Update dependencies in the go.mod file: Upstream usually uses scripts to bump all k8s dependencies at once.
    However the same can be achieved manually with a simple replace statement. For example, to update the k8s.io dependency list, you can replace all occurrences of the 0.y.z version with the latest 0.y.z version. For example:
    ```go.mod
    k8s.io/api v0.28.0
    k8s.io/apiextensions-apiserver v0.28.0
    # .. do this for all k8s.io dependencies
    ```
    becomes
    ```go.mod
    k8s.io/api v0.29.0
    k8s.io/apiextensions-apiserver v0.29.0
    # .. do this for all k8s.io dependencies
    ```
3. Make sure that all Makefile dependency versions are up to date: The operator makes use of controller-tools, operator-sdk and envtest for various non-compile-time tasks such as generating yaml or testing.
    Make sure that the versions of these dependencies are up to date in the Makefile.
    [They are found at the start of the Makefile](https://github.com/openshift/lvm-operator/blob/v4.16.0/Makefile#L43-L46).
    Note that some of these versions are getting their correct tag from the go.mod file.
    In this case, the go.mod dependency is the single source of truth and and upgrade there also will update the relevant client tools (e.g. the ginkgo testing CLI gets its version from the dependency of the go.mod file so as to not maintain the version twice)
4. Run `make godeps-update docker-build` to update the dependencies as well as to build the operator image with the updated dependencies.
    This is a good first check if something broke during the upgrade. If it did, this is the time to debug further.
    If you are facing trouble, go slowly by upgrading one dependency group at a time and testing the operator to ensure that the operator is still functioning as expected.
    If in doubt, revert the changes partially and attempt again.
    In Kubernetes and CSI sometimes releases may be out of sync and need patching. Similarly you might have to issue
    replace statements in the module file in case of a broken transitive module file (for example because it includes a redacted release version that now needs to be explicitly overwritten or replaced)
    For this reason, it is also good to keep track of existing overwrites and replace statements to replace them later on in case they are no longer needed after an update.
5. Add any custom patches that may be necessary as part of the upgrade.
    Sometimes, the operator may need to be patched to work with the latest version of the dependencies due to transitive dependencies or other reasons.
    Make sure to add these patches as part of the upgrade process by adding them to the go dependency update make target.
    For example, if you need to patch the k8s.io/api package, you can add a patch to the `go mod edit` command in the `godeps-update` target in the Makefile.
    For example, to patch the vendored external provisioner with a file from the hack folder in `hack/external-provisioner.patch`:
    ```Makefile
    godeps-update: ## Run go mod tidy and go mod vendor.
        go mod tidy && go mod vendor
        patch -p1 -d $(SELF_DIR)vendor/github.com/kubernetes-csi/external-provisioner/v5 < $(SELF_DIR)hack/external-provisioner.patch
    ```
   This will use a regular file patch (which you can generate manually or preferably with an IDE as an exported patch from your clipboard) and will allow you to surgically update any discrepancies in the vendored code without having to wait for the vendor to update.

## Expected Replacement of TopoLVM

TopoLVM is a dependency of the LVM Operator. Unlike many CSI driver metadata operators, LVM Operator starts the TopoLVM controllers in its own code of vgmanager to save resources and allow faster startups. As such, a version of TopoLVM is hardcompiled into the LVM Operator.
This means that LVM Operator contains a reference to topolvm via `go.mod` and `go.sum`.

When updating the LVM Operator, it is expected that the TopoLVM version is also updated to the latest version.
Note that the go.mod version contains not just the direct dependency, but also a replacement to https://github.com/openshift/topolvm.
The versions should ideally align with each other and be go API compatible to avoid issues and are mostly present to be able to patch openshift/topolvm in case of a critical issue that is not able to be fixed in upstream in time and needs immediate remediation.
