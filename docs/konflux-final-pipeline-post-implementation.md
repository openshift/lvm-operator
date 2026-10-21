# Konflux Final Pipelines — Post-Implementation Checklist

This document covers the steps needed after both final pipelines are merged:

- `.tekton/operator-version-bump-production-final-pipeline.yaml` (**pipeline A**) — runs after the operator's production release. Bumps `OPERATOR_VERSION` and opens a PR. Does not touch catalog templates.
- `.tekton/catalog-candidate-update-staging-final-pipeline.yaml` (**pipeline B**) — runs after the `lvm-operator-bundle` component's staging release. Runs `generate_catalog_template.sh` and opens/updates a PR if anything changed. This is the only pipeline that regenerates catalog templates — merging pipeline A's version-bump PR triggers the normal build cascade (operator rebuild → bundle rebuild → bundle staging release), which reaches pipeline B.

Both pipelines create commits via the GitHub GraphQL `createCommitOnBranch` mutation rather than `git push`, so commits are signed/verified by GitHub automatically — no GPG/SSH signing key to provision.

## 1. Configure GitHub App Credentials

Create a Kubernetes secret in the Konflux tenant namespace (`logical-volume-manag-tenant`) with your GitHub App credentials. Both pipelines share this one secret.

**Secret name:** `github-app-credentials`

**Required keys:**
- `app-id` — The numeric GitHub App ID
- `installation-id` — The numeric installation ID for the App on this repo/org
- `private-key` — The PEM-encoded private key for the GitHub App

This matches the verified working pattern from `openshift/multiarch-tuning-operator`'s `fbc-update-final-pipeline.yaml`. Each pipeline generates a JWT (signed with `private-key`), exchanges it for an installation access token via `POST /app/installations/$INSTALLATION_ID/access_tokens`, and uses that token to call the GitHub REST/GraphQL API directly (no `git push`, no `gh` CLI).

**Required App permissions:** Contents (read/write), Pull Requests (read/write).

**Setup steps:**
1. Create (or identify) a GitHub App with write access to the `openshift/lvm-operator` repository
2. Ensure the App has the permissions above
3. Install the App on the `openshift/lvm-operator` repository
4. Generate a private key from the App settings page
5. Look up the installation ID (via `GET /app/installations` using a JWT, or from the URL when viewing the app's installation settings)
6. Create the secret in the tenant namespace:
   ```bash
   kubectl create secret generic github-app-credentials \
     --namespace logical-volume-manag-tenant \
     --from-literal=app-id=YOUR_APP_ID \
     --from-literal=installation-id=YOUR_INSTALLATION_ID \
     --from-file=private-key=path/to/private-key.pem
   ```

## 2. Registry Access for Pipeline B

`generate_catalog_template.sh` calls `skopeo list-tags`/`skopeo inspect` against two registries that both need auth:
- `quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle` — a private Konflux tenant workload repo (not public), used for candidate tag/digest discovery
- `registry.redhat.io` — used to determine which versions are already officially released

Pipeline B mounts two existing tenant-namespace secrets as volumes:
- `registry-redhat-io-docker` for `registry.redhat.io`
- `components-namespace-pull` for the Quay tenant workload repo

Both are standard `kubernetes.io/dockerconfigjson` image pull secrets. Since `skopeo`'s `REGISTRY_AUTH_FILE` only accepts a single file but both registries need auth in the same script run, the pipeline merges the two secrets' `.dockerconfigjson` `auths` objects into one combined file (`jq -s '{auths: (.[0].auths + .[1].auths)}' ...`) before exporting `REGISTRY_AUTH_FILE`. No action required here beyond confirming both secrets already exist in `logical-volume-manag-tenant` (they do, per team confirmation) and have valid, non-expired credentials.

**Note:** `registry.stage.redhat.io` appears in the script only as a string written into the candidate template's output YAML (line 86 of `generate_catalog_template.sh`) — it's never queried over the network, so the also-available `registry-stage-redhat-io-docker` secret isn't currently wired in. If a future change reintroduces a live staging-registry lookup, add it to the same merge.

Pipeline A needs no registry access at all — it only reads/writes `release/container-build.args` via the GitHub API.

## 3. Wire Up Both ReleasePlans

**Pipeline A** — attach as the `finalPipeline` on the operator's production `ReleasePlan`:

```yaml
finalPipeline:
  pipelineRef:
    resolver: git
    params:
      - name: url
        value: https://github.com/openshift/lvm-operator.git
      - name: revision
        value: release-5.0   # branch where the pipeline YAML lives
      - name: pathInRepo
        value: .tekton/operator-version-bump-production-final-pipeline.yaml
  params:
    - name: release-branch
      value: release-5.0
    # Omit next-version to auto-increment patch (5.0.0 → 5.0.1)
    # Or set explicitly:
    # - name: next-version
    #   value: "5.0.1"
```

**Pipeline B** — attach as the `finalPipeline` on a separate `ReleasePlan` governing the `lvm-operator-bundle` component's **staging** release:

```yaml
finalPipeline:
  pipelineRef:
    resolver: git
    params:
      - name: url
        value: https://github.com/openshift/lvm-operator.git
      - name: revision
        value: release-5.0
      - name: pathInRepo
        value: .tekton/catalog-candidate-update-staging-final-pipeline.yaml
  params:
    - name: release-branch
      value: release-5.0
    - name: y-stream
      value: v5.0
```

Pipeline B is intentionally **not** also wired to the bundle's production ReleasePlan — the release template will pick up a production promotion opportunistically the next time pipeline B runs from a staging event, which was an accepted trade-off (see the design decision log in the plan history / PR description).

## 4. No Placeholder Entries — Verify Candidate Template Behavior Instead

Earlier drafts of this design considered writing a placeholder entry into the candidate template for mintmaker to later pin to a digest. That approach was dropped after verifying:

- `renovate.json`'s mintmaker custom manager only refreshes digests that already exist in `lvm-operator-catalog-template.yaml` (its `managerFilePatterns` regex does not match the candidate template's filename), and only for entries that already have a digest — it cannot resolve a bare tag.
- Konflux's native component-nudge system has the same limitation per its docs: "Konflux only updates image digest references. If images use tag references, these will not be updated."
- Testing `prepare-catalog.sh`'s merge command locally with `yq` confirmed it does **not** deduplicate array entries — manually inserting a duplicate-version entry would produce two bundle entries for the same version after merge.

So: no pipeline ever writes a placeholder. Pipeline A leaves the candidate template alone (often ending up empty right after a release, which is harmless). Pipeline B is the only place that writes to it, always with a real, already-resolved digest from `generate_catalog_template.sh`'s live registry queries.

**Post-merge verification:** confirm that once pipeline B's PR merges with real candidate content, mintmaker's existing digest-refresh behavior (for `lvm-operator-catalog-template.yaml`, not the candidate file) continues to work as before — this design doesn't change that mechanism, just avoids relying on it for anything new.

## 5. Version Bump Scope (Pipeline A)

Pipeline A only bumps `OPERATOR_VERSION` in `release/container-build.args`. For a minor version bump (e.g. 5.0 → 5.1), you may also need to update:
- `LVMS_TAGS` (e.g. `v5.0` → `v5.1`)
- `OPENSHIFT_VERSIONS` (e.g. `v5.0-v5.1` → `v5.1-v5.2`)
- `Y_STREAM` default in `release/konflux.make`, and the `y-stream` param on pipeline B's ReleasePlan

For patch bumps within the same Y-stream, only `OPERATOR_VERSION` needs updating — this is what pipeline A automates.

## 6. Testing

Both pipelines depend on Konflux-managed ReleasePlans and can only be fully tested through a real release cycle.

**Pipeline A:**
1. Trigger a test production release
2. Verify the PR it opens bumps `OPERATOR_VERSION` correctly and targets the right release branch
3. Merge it and confirm the operator build, then bundle build, both trigger as expected
4. Confirm the resulting commit shows as "Verified" on GitHub

**Pipeline B:**
1. Trigger (or wait for) a bundle staging release
2. Verify the PR it opens/updates contains the regenerated catalog templates
3. Run it twice in a row against unchanged registry state and confirm the second run skips cleanly (no duplicate PR, no empty commit) — check the `CHANGED` result is `"false"`
4. Confirm a later run correctly reuses the same branch/PR rather than opening a new one
5. Confirm the resulting commit shows as "Verified" on GitHub
