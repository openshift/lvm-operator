---
status: Proposed
date: 2026-07-20
decision-makers: Jeff Roche (jeroche), Pablo Acevedo Montserrat (pacevedom)
consulted: LVMS team
informed: OpenShift Edge team
---

# Konflux Final Pipelines for Release Automation

## Context and Problem Statement

After the LVM Operator was onboarded to Konflux, every production
release required several manual steps:

1. Bump `OPERATOR_VERSION` in `release/container-build.args` on the
   release branch.
2. Wait for the build cascade (operator rebuild, bundle rebuild, bundle
   staging release).
3. Run `make catalog-template` to regenerate the FBC catalog semver
   templates with new digests from the staging and production registries.
4. Open PRs for each of the above and get them merged.

These steps were error-prone (wrong branch, stale digests, forgotten
version bumps) and created delays between a production release and the
availability of the next development cycle. The team needs a way to
automate the post-release housekeeping while staying within Konflux's
architecture.

## Decision Drivers

* Post-release steps are mechanical and well-defined — ideal for
  automation.
* Commits to `openshift/lvm-operator` must be verified/signed to
  satisfy branch protection rules.
* The automation must not require provisioning GPG/SSH signing keys in
  the Konflux tenant — key management is a maintenance burden and a
  security surface.
* Catalog template regeneration requires querying both
  `registry.redhat.io` and the private Quay tenant repo for bundle
  digests — the automation needs registry credentials.
* The version bump and catalog template update are causally linked
  (bump triggers the build cascade which triggers the template update)
  but should remain decoupled so each can be triggered and tested
  independently.
* Mintmaker already handles digest-only updates to the candidate
  template via nudge PRs, but it cannot handle version bumps or full
  template regeneration.

## Considered Options

1. **Manual process** — continue as-is with documented runbooks.
2. **Prow postsubmit jobs** — run shell scripts after merges to the
   release branch (similar to the existing `manage-release.sh` pattern
   on the `release-management` branch).
    - Rejected due to not being able to trigger a prow job at the correct
      step in the release process (no valid triggers exist for this).
3. **GitHub Actions workflows** — use GitHub-native CI to run the
   post-release steps.
    - Rejected in favor of doing the same thing in Konflux to reduce the
      number of CI systems we have to maintain. There are also security
      implications of using GitHub actions for this as we would need to
      add some secrets. There also does not exist a valid trigger for
      these jobs.
4. **Konflux final pipelines** — use the `finalPipeline` field on
   `ReleasePlan` resources to trigger Tekton pipelines after successful
   releases, with commits created via the GitHub GraphQL API.

## Decision Outcome

Chosen option: "Konflux final pipelines", because they integrate
directly with the release lifecycle (triggered by the release system
itself, not by branch merges), keep all automation within the Konflux
ecosystem, and avoid the need for signing keys by using GitHub's
`createCommitOnBranch` GraphQL mutation which produces server-side
verified commits.

Two pipelines were implemented:

**Pipeline A** (`operator-version-bump-production-final-pipeline`) —
wired to the operator's **production** `ReleasePlan`. After a
successful production release, it:

1. Reads the current `OPERATOR_VERSION` from `container-build.args`
   on the release branch via the GitHub API.
2. Auto-increments the patch version (or uses an explicit override).
3. Creates a signed commit on a PR branch via the GitHub GraphQL API.
4. Opens (or reuses) a PR with `approved` + `lgtm` labels.

Merging this PR triggers the normal build cascade: operator rebuild,
bundle rebuild, bundle staging release — which reaches pipeline B.

**Pipeline B** (`catalog-candidate-update-staging-final-pipeline`) —
wired to the bundle's **staging** `ReleasePlan`. After a bundle
reaches staging, it:

1. Clones the repo, runs `make catalog-template` with registry
   credentials to regenerate both catalog template files.
2. Diffs the result — if nothing changed, exits cleanly.
3. Creates a signed commit via the GraphQL API and opens/updates a
   persistent PR.

### Key Design Decisions Within This ADR

**GitHub App authentication over PATs or deploy keys.** A GitHub App
installation token provides scoped, short-lived credentials (Contents
and Pull Requests read/write) without tying automation to a personal
account. The App's private key is stored as a single Kubernetes secret
(`github-app-credentials`) shared by both pipelines.

**GraphQL `createCommitOnBranch` over `git push`.** Commits created
via this mutation are signed by GitHub server-side, producing verified
commits without provisioning GPG or SSH keys. This also eliminates the
need for a full git clone in pipeline A (it only reads/writes one file
via the REST API).

**Pipeline B is wired to staging, not production.** The candidate
template must be updated every time a new bundle SHA reaches staging,
not just on production releases. This includes digest-only rebuilds
(dependency updates, base image refreshes). Wiring to staging ensures
the catalog always reflects the latest staging bundle. Pipeline B is
intentionally not also wired to the bundle's production `ReleasePlan`
— a production release will opportunistically update the released
template the next time pipeline B runs from a staging event.

**Split template files.** The catalog uses two semver template files
(released and candidate) so that Mintmaker's nudge PRs only touch the
candidate template's digest without overwriting the released bundle
references. Pipeline B is the only automation that runs the full
`generate_catalog_template.sh` which queries both registries and
regenerates both files.

**Fail-closed on API/registry errors.** Both pipelines use `set -euo
pipefail` and validate every API response. If the GitHub token
exchange, commit creation, or registry query fails, the pipeline fails
rather than silently producing incomplete results.

### Consequences

* Good, because post-release version bumps and catalog updates happen
  automatically within minutes of a release, eliminating manual toil
  and reducing time-to-next-development-cycle.
* Good, because all automation commits are verified/signed without any
  key management burden.
* Good, because the two pipelines are decoupled — pipeline A can be
  tested independently from pipeline B, and each can fail without
  blocking the other.
* Good, because pipeline B is idempotent — running it twice against
  unchanged registry state produces no duplicate PRs or empty commits.
* Bad, because the pipelines depend on a GitHub App secret that must
  be provisioned and kept valid.
* Bad, because full end-to-end testing requires a real release cycle —
  the pipelines cannot be unit-tested in isolation.
* Neutral, because pipeline B requires registry credentials for both
  `registry.redhat.io` and the private Quay tenant repo, adding two
  more secrets to manage. Both secrets already exist in the konflux 
  tenant we use and can be reused.

## More Information

* [Catalog lifecycle documentation](../konflux/file-based-catalog.md) —
  how the catalog templates are maintained across release scenarios.
* [Release management documentation](../konflux/release-management.md) —
  the broader production release process.
* [Konflux final pipeline docs](https://konflux-ci.dev/docs/releasing/create-release-plan/) —
  upstream documentation on ReleasePlan finalPipeline configuration.
* The `multiarch-tuning-operator` repo's `fbc-update-final-pipeline.yaml`
  served as the reference implementation for the GitHub App + GraphQL
  commit pattern.
