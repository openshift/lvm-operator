You are helping the user set up prerequisites to test unreleased LVMS operator builds on OpenShift clusters. This command supports two deployment scenarios:

1. **Connected cluster**: Using CatalogSource and ImageDigestMirrorSet (IDMS)
2. **Disconnected cluster**: Using oc-mirror to mirror images to a disconnected registry

## How to Run This Command

This is a Claude Code slash command. To use it, type the following in your Claude Code chat:

```
/setup-lvms-prereq
```

You can also provide initial information inline if you prefer. Examples:

**Connected cluster example:**
```
/setup-lvms-prereq connected
```

**Disconnected cluster example:**
```
/setup-lvms-prereq disconnected
```

The assistant will then interactively ask for any missing information needed to complete the setup.

## Required Information
Ask the user for:
1. **Deployment type**: Connected or Disconnected cluster
2. **Kubeconfig path**: Path to the kubeconfig file for the target cluster (default: `~/.kube/config`)
3. **If Connected**: Catalog image to use (e.g., `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-catalog@sha256:...`)
   - To get the catalog image, insert the snapshot name given by the developer in this URL and browse through it: `<konflux_prod_url>/ns/logical-volume-manag-tenant/applications/lvm-operator-catalog-<OCP_VERSION>/snapshots/<SNAPSHOT_NAME>`
4. **If Disconnected**:
   - OCP version (e.g., 4.17, 4.18, 4.19, 4.20)
   - Mirror registry URL (e.g., `registry.example.com:5000`) - Note: If using flexy-install, the mirror registry name can be retrieved from the flexy-install console by searching for the keyword `Mirroring`
   - Path to registry credentials file (default: `${XDG_RUNTIME_DIR}/containers/auth.json`)
   - Catalog image to mirror (e.g., `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-catalog@sha256:...`)
     - To get the catalog image, insert the snapshot name given by the developer in this URL and browse through it: `<konflux_prod_url>/ns/logical-volume-manag-tenant/applications/lvm-operator-catalog-<OCP_VERSION>/snapshots/<SNAPSHOT_NAME>`

## Steps to Execute

### Common Validation Steps (Both Connected and Disconnected)

1. **Validate inputs**:
   - Check if kubeconfig file exists: `ls -l <kubeconfig-path> && echo "Kubeconfig found" || echo "Error: Kubeconfig not found"`
   - Verify `oc` command is available: `which oc` or `oc version --client`
   - Test cluster connectivity: `oc whoami --kubeconfig=<kubeconfig-path>`
   - Validate catalog image format (should contain registry/repo@sha256: or :tag): Check that the image string matches expected pattern
   - **If Disconnected**: Verify `oc-mirror` is available: `which oc-mirror` or `oc-mirror version`
   - **If Disconnected**: Check registry credentials file exists: `ls -l <credentials-path> && echo "Credentials found" || echo "Error: Credentials not found"`
   - **If Disconnected**: Inform the user to ensure mirror registry credentials are added to their credentials file (either `${XDG_RUNTIME_DIR}/containers/auth.json` or the custom path they specified). Registry credentials can be retrieved by running: `oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}' --kubeconfig=<kubeconfig-path> | base64 -d`

---

## Flow A: Connected Cluster Deployment (IDMS + CatalogSource)

Execute these steps if deployment type is "Connected":

2. **Clean up existing resources**:
   - Check and remove `qe-app-registry` CatalogSource if present: `oc delete catalogsource qe-app-registry -n openshift-marketplace --kubeconfig=<kubeconfig-path> --ignore-not-found=true`
   - Check for ImageContentSourcePolicy resources: `oc get imagecontentsourcepolicy --kubeconfig=<kubeconfig-path>`
   - If any ImageContentSourcePolicy exists, warn the user and delete them: `oc delete imagecontentsourcepolicy --all --kubeconfig=<kubeconfig-path>`
   - Note: Deleting ICSP may trigger MCP update, so wait for MCP to stabilize if needed

3. **Create CatalogSource manifest** in `/tmp/lvms-catalogsource.yaml`:
   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: lvms-custom-catalog
     namespace: openshift-marketplace
   spec:
     displayName: konflux
     publisher: OpenShift QE
     sourceType: grpc
     updateStrategy:
       registryPoll:
         interval: 15m
     image: <CATALOG_IMAGE>
   ```

4. **Create ImageDigestMirrorSet manifest** in `/tmp/lvms-idms.yaml`:
   ```yaml
   apiVersion: config.openshift.io/v1
   kind: ImageDigestMirrorSet
   metadata:
     name: lvm-operator-imagedigestmirrors
   spec:
     imageDigestMirrors:
       - mirrors:
         - registry.stage.redhat.io/lvms4/lvms-operator-bundle
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
         source: registry.redhat.io/lvms4/lvms-operator-bundle
       - mirrors:
         - registry.stage.redhat.io/lvms4/lvms-rhel9-operator
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
         source: registry.redhat.io/lvms4/lvms-rhel9-operator
       - mirrors:
         - registry.stage.redhat.io/lvms4/lvms-must-gather-rhel9
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
         source: registry.redhat.io/lvms4/lvms-must-gather-rhel9
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
         source: registry.stage.redhat.io/lvms4/lvms-operator-bundle
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
         source: registry.stage.redhat.io/lvms4/lvms-rhel9-operator
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
         source: registry.stage.redhat.io/lvms4/lvms-must-gather-rhel9
   ```

5. **Apply the IDMS and monitor MCP status**:
   - Warn the user: "Applying IDMS will trigger node reboots. This may take 20-30 minutes."
   - Apply the ImageDigestMirrorSet: `oc apply -f /tmp/lvms-idms.yaml --kubeconfig=<kubeconfig-path>`
   - Wait for MCP to start updating: `oc wait mcp/master mcp/worker --for=condition=Updating --timeout=5m --kubeconfig=<kubeconfig-path>`
   - Monitor MCP status: `oc get mcp --kubeconfig=<kubeconfig-path>` (show current status)
   - Wait for MCP to finish updating: `oc wait mcp/master mcp/worker --for=condition=Updated --for=condition=Updating=False --timeout=30m --kubeconfig=<kubeconfig-path>`
   - Verify final MCP status: `oc get mcp --kubeconfig=<kubeconfig-path>`

6. **Apply the CatalogSource**:
   - Apply the CatalogSource: `oc apply -f /tmp/lvms-catalogsource.yaml --kubeconfig=<kubeconfig-path>`
   - Wait for CatalogSource to be ready: `oc wait catalogsource/lvms-custom-catalog -n openshift-marketplace --for=jsonpath='{.status.connectionState.lastObservedState}'=READY --timeout=5m --kubeconfig=<kubeconfig-path>`

7. **Verify installation**:
   - Check CatalogSource status: `oc get catalogsource lvms-custom-catalog -n openshift-marketplace --kubeconfig=<kubeconfig-path>`
   - Verify packagemanifest: `oc get packagemanifest lvms-operator --kubeconfig=<kubeconfig-path>`

8. **Cleanup and next steps**:
   - Optionally remove temporary files: `/tmp/lvms-catalogsource.yaml` and `/tmp/lvms-idms.yaml`
   - Inform the user: "Prerequisites are set up. You can now install the unreleased LVMS operator from OperatorHub or create a Subscription."

---

## Flow B: Disconnected Cluster Deployment (oc-mirror)

**Note**: `oc-mirror` may not work reliably on macOS systems. For best results, use Fedora or RHEL machines to ensure oc-mirror runs properly.

Execute these steps if deployment type is "Disconnected":

2. **Create ImageSetConfiguration for catalog** in `/tmp/lvms-catalog-config.yaml`:
   ```yaml
   kind: ImageSetConfiguration
   apiVersion: mirror.openshift.io/v2alpha1
   mirror:
     operators:
       - catalog: <CATALOG_IMAGE>
         packages:
           - name: lvms-operator
             channels:
               - name: 'stable-<OCP_VERSION>'
   ```
   Replace `<CATALOG_IMAGE>` with the catalog image and `<OCP_VERSION>` with the OCP version (e.g., 4.18).

3. **Run oc-mirror to mirror catalog**:
   - Create working directory: `mkdir -p /tmp/oc-mirror-workspace`
   - Run oc-mirror: `oc-mirror -c /tmp/lvms-catalog-config.yaml --workspace file:///tmp/oc-mirror-workspace docker://<MIRROR_REGISTRY> --v2 --dest-tls-verify=false`
   - Replace `<MIRROR_REGISTRY>` with the mirror registry URL
   - Note: Use `--dest-tls-verify=false` if the mirror registry uses self-signed certificates (otherwise use `--dest-tls-verify=true`)
   - This will mirror the catalog and attempt to mirror all related operator images
   - Check for mirroring errors: `ls -ltr /tmp/oc-mirror-workspace/working-dir/logs/mirroring_errors_*.txt`

4. **Apply the generated CatalogSource**:
   - Find the CatalogSource manifest: `ls /tmp/oc-mirror-workspace/working-dir/cluster-resources/cs-*.yaml`
   - Apply CatalogSource: `oc apply -f /tmp/oc-mirror-workspace/working-dir/cluster-resources/cs-*.yaml --kubeconfig=<kubeconfig-path>`
   - Wait for CatalogSource to be ready: `oc wait catalogsource -n openshift-marketplace --all --for=jsonpath='{.status.connectionState.lastObservedState}'=READY --timeout=5m --kubeconfig=<kubeconfig-path>`

5. **Handle mirroring errors and create additional images config** (only if mirroring errors exist):
   - Find the latest error log: `ERROR_LOG=$(ls -t /tmp/oc-mirror-workspace/working-dir/logs/mirroring_errors_*.txt 2>/dev/null | head -1)`
   - If error log exists, extract failed images and create corrected image references:
     - Parse lines containing "error mirroring image docker://registry.redhat.io/lvms4/"
     - Extract the image name and SHA from each error line
     - Transform image references:
       - Replace `registry.redhat.io/lvms4/lvms-operator-bundle` → `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle`
       - Replace `registry.redhat.io/lvms4/lvms-rhel9-operator` → `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator`
       - Replace `registry.redhat.io/lvms4/lvms-must-gather-rhel9` → `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather`
   - Create `/tmp/lvms-additional-images-config.yaml`:
   ```yaml
   kind: ImageSetConfiguration
   apiVersion: mirror.openshift.io/v2alpha1
   mirror:
     additionalImages:
       - name: quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle@sha256:<SHA_FROM_LOG>
       - name: quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator@sha256:<SHA_FROM_LOG>
       - name: quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather@sha256:<SHA_FROM_LOG>
   ```
   Where `<SHA_FROM_LOG>` is extracted from the error log for each respective image.

6. **Run oc-mirror for additional images** (only if step 5 was executed):
   - Run oc-mirror: `oc-mirror -c /tmp/lvms-additional-images-config.yaml --workspace file:///tmp/oc-mirror-workspace docker://<MIRROR_REGISTRY> --v2 --dest-tls-verify=false`
   - This will mirror the additional images with correct registry references from quay.io

7. **Create ImageDigestMirrorSet manifest** in `/tmp/lvms-idms.yaml`:
   ```yaml
   apiVersion: config.openshift.io/v1
   kind: ImageDigestMirrorSet
   metadata:
     name: lvm-operator-imagedigestmirrors
   spec:
     imageDigestMirrors:
       - mirrors:
         - <MIRROR_REGISTRY>/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
         source: registry.redhat.io/lvms4/lvms-operator-bundle
       - mirrors:
         - <MIRROR_REGISTRY>/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
         source: registry.redhat.io/lvms4/lvms-rhel9-operator
       - mirrors:
         - <MIRROR_REGISTRY>/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
         source: registry.redhat.io/lvms4/lvms-must-gather-rhel9
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
         source: registry.stage.redhat.io/lvms4/lvms-operator-bundle
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
         source: registry.stage.redhat.io/lvms4/lvms-rhel9-operator
       - mirrors:
         - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
         source: registry.stage.redhat.io/lvms4/lvms-must-gather-rhel9
   ```
   Replace `<MIRROR_REGISTRY>` with the mirror registry URL.

8. **Apply the IDMS and monitor MCP status**:
   - Warn the user: "Applying IDMS will trigger node reboots. This may take 20-30 minutes."
   - Apply ImageDigestMirrorSet: `oc apply -f /tmp/lvms-idms.yaml --kubeconfig=<kubeconfig-path>`
   - Wait for MCP to start updating: `oc wait mcp/master mcp/worker --for=condition=Updating --timeout=5m --kubeconfig=<kubeconfig-path>`
   - Monitor MCP status: `oc get mcp --kubeconfig=<kubeconfig-path>`
   - Wait for MCP to finish updating: `oc wait mcp/master mcp/worker --for=condition=Updated --for=condition=Updating=False --timeout=30m --kubeconfig=<kubeconfig-path>`
   - Verify final MCP status: `oc get mcp --kubeconfig=<kubeconfig-path>`

9. **Verify installation**:
   - Check CatalogSource status: `oc get catalogsource -n openshift-marketplace --kubeconfig=<kubeconfig-path>`
   - Verify packagemanifest: `oc get packagemanifest lvms-operator --kubeconfig=<kubeconfig-path>`

10. **Cleanup and next steps**:
   - Optionally remove temporary files: `/tmp/lvms-imageset-config.yaml` and `/tmp/oc-mirror-workspace/`
   - Inform the user: "Prerequisites are set up for disconnected cluster. You can now install the LVMS operator from OperatorHub or create a Subscription."

---

## Important Notes
- The ImageDigestMirrorSet will trigger a MachineConfigPool update, which will reboot cluster nodes
- Deleting ImageContentSourcePolicy resources will also trigger MCP updates
- The MCP update process can take 20-30 minutes depending on cluster size
- The user should ensure they have cluster-admin permissions
- Always wait for MCP to start updating before waiting for completion to avoid false positives

## Error Handling

### Common Errors (Both Flows)
- If kubeconfig file doesn't exist, inform user and ask for correct path
- If catalog image format is invalid, ask for correction
- If `oc` is not available, inform the user to install OpenShift CLI
- If `oc whoami` fails, verify kubeconfig is valid and cluster is accessible
- If MCP doesn't start updating within 5 minutes, check: `oc get mcp -o yaml --kubeconfig=<kubeconfig-path>`
- If MCP update times out, provide manual check instructions: `oc get mcp --kubeconfig=<kubeconfig-path>` and `oc get nodes --kubeconfig=<kubeconfig-path>`

### Disconnected Flow Specific Errors
- If `oc-mirror` is not available, inform the user to install it from the OpenShift mirror downloads
- If registry credentials file doesn't exist, ask for correct path or create one using `podman login <MIRROR_REGISTRY>`
- If oc-mirror fails with authentication errors, verify credentials: `cat <credentials-path>` and ensure registry is accessible
- If oc-mirror fails during mirroring, check disk space in `/tmp/oc-mirror-workspace` and registry connectivity
- If IDMS manifests are not generated, check oc-mirror logs in the workspace directory
- If CatalogSource fails to apply, verify the mirrored catalog image exists in the mirror registry
