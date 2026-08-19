# Fix: Inconsistent Default LVMCluster Name in Console "Create Instance" Flows

**Bug:** [OCPBUGS-86002](https://redhat.atlassian.net/browse/OCPBUGS-86002)

## Problem

The OpenShift Console shows a different default name for the sample `LVMCluster`
depending on which "Create instance" entry point the user takes:

1. Right after installing the operator, the Console's "Create LVMCluster" quick
   action (shown on the Installed Operators overview, before drilling into APIs
   Provided) offers a CR named `test-lvmcluster`.
2. Going to *Installed Operators → LVM Storage → LVMCluster tab → Create
   LVMCluster* instead offers a CR named `my-lvmcluster`.

Users expect the same default name from both flows.

## Root Cause

Two independent OLM annotations on the CSV each carry their own hand-authored
sample `LVMCluster` CR, and the two have drifted out of sync:

- **`alm-examples`** — generated during `make bundle` from
  `config/samples/lvm_v1alpha1_lvmcluster.yaml` (name `my-lvmcluster`, with
  `default: true`, `fstype: xfs`, `thinPoolConfig.overprovisionRatio: 10`,
  `thinPoolConfig.sizePercent: 90`). This drives flow #2 above (the standard
  "create from APIs Provided" form, which Console populates from `alm-examples`).
- **`operatorframework.io/initialization-resource`** — a JSON literal written
  directly into
  `config/manifests/bases/lvms-operator.clusterserviceversion.yaml:12-30`
  (name `test-lvmcluster`, missing `default`/`fstype`, no
  `overprovisionRatio`/`sizePercent`). This drives flow #1 above (Console's
  "initialization resource" quick-create prompt shown right after install).

Both annotations end up in the rendered
`bundle/manifests/lvms-operator.clusterserviceversion.yaml`. Neither is
generated from the other, so a past edit to one sample (adding
`my-lvmcluster`'s richer fields) was never mirrored into the other.

## Fix

The bug report is specifically about the name mismatch, not about the two
samples' other fields. Change only `metadata.name` in the
`initialization-resource` annotation:

- `test-lvmcluster` → `my-lvmcluster`.

Leave `deviceClasses[0]` and `thinPoolConfig` exactly as they are today (no
`default`, no `fstype`, no `overprovisionRatio`/`sizePercent`). Adding those
would change the actual default storage configuration Console presents in the
initialization-resource quick-create flow — a real behavior change beyond
what was reported, not just a display fix. This is a single-line rename in
`config/manifests/bases/lvms-operator.clusterserviceversion.yaml` — no Go
code, no controller logic, no build tooling changes.

Add a short note near the annotation (as close as the YAML/JSON structure
allows without polluting the rendered CSV) stating that the `metadata.name`
here must be kept in sync with `config/samples/lvm_v1alpha1_lvmcluster.yaml`,
so the next person editing either sample's name remembers to update both.

### Explicitly out of scope

- Not syncing `default`/`fstype`/`overprovisionRatio`/`sizePercent` or any
  other spec fields between the two samples — only the reported name
  mismatch is being fixed. The two samples remain intentionally free to
  differ in content; only their name must match.
- Not restructuring the build to generate `initialization-resource` from the
  sample file automatically (that's a heavier, build-pipeline-touching fix —
  worth a follow-up issue if this class of bug recurs, but disproportionate
  for this ticket).
- Not changing `config/samples/lvm_v1alpha1_lvmcluster.yaml` itself.

## Regeneration and Verification

Per `AGENTS.md`, any change affecting CRDs/CSV/RBAC requires regenerating
derived manifests:

```bash
make bundle
make catalog
```

This updates `bundle/manifests/lvms-operator.clusterserviceversion.yaml` and
the catalog so both `alm-examples` and `initialization-resource` reference an
`LVMCluster` named `my-lvmcluster` (their other spec fields remain
intentionally different). Confirm via:

```bash
git diff bundle/manifests/lvms-operator.clusterserviceversion.yaml
```

Then run `make verify` (which includes `hack/verify-bundle.sh` and
`hack/verify-catalog.sh`) to confirm the regenerated files match what's
committed.

## Testing

This is a manifest-only fix with no controller or reconciliation behavior
change, so no new unit tests are required. Verification is the `make verify`
gate above, plus a manual check (or e2e, if desired) that both Console
"Create LVMCluster" entry points now show the same default name,
`my-lvmcluster`.
