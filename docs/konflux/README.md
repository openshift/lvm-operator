# Konflux Based Build and Release Management

This and the linked documents describe the Konflux CI/CD system that builds, tests, and
releases the LVMS container images. All build pipelines are defined as Tekton resources in
the `.tekton/` directory, with supporting Dockerfiles, scripts, and configuration in the
`release/` directory.

## Related Topics

For detailed information on the OLM Bundle lifecycle, see
[operator-bundle-lifecycle.md](./operator-bundle-lifecycle.md)

For detailed information on the Catalog Build lifecycle, see
[file-based-catalog.md](./file-based-catalog.md)

For information on managing production releases, see
[release-management.md](./release-management.md)

## Overview

LVMS uses [Konflux](https://konflux-ci.dev/) (Red Hat's build system built on Tekton and
Pipelines as Code) to produce four container images from a single monorepo:

| Component   | Image                  | Architectures                  | Pipeline                          |
| ----------- | ---------------------- | ------------------------------ | --------------------------------- |
| Operator    | `lvm-operator`         | x86_64, arm64, ppc64le, s390x  | `multi-arch-build-pipeline`       |
| OLM Bundle  | `lvm-operator-bundle`  | x86_64                         | `single-arch-build-pipeline`      |
| FBC Catalog | `lvm-operator-catalog` | x86_64, arm64, ppc64le, s390x  | `catalog-patching-build-pipeline` |
| Must-Gather | `lvms-must-gather`     | x86_64, arm64, ppc64le, s390x  | `multi-arch-build-pipeline`       |

All images are pushed to `quay.io/redhat-user-workloads/logical-volume-manag-tenant/`
and flow through the Red Hat release pipeline to their production locations under
`registry.redhat.io/lvms4/`.

### Key Konflux Concepts Utilized

- [Konflux Configuration as Code](https://konflux-ci.dev/docs/building/configuration-as-code/)
- [Hermetic (offline) builds](https://konflux-ci.dev/docs/building/hermetic-builds/)
- [Prefetching build dependencies](https://konflux-ci.dev/docs/building/prefetching-dependencies/)
- [Trusted Artifacts](https://konflux-ci.dev/docs/building/using-trusted-artifacts/)
- [Secrets in Konflux](https://konflux-ci.dev/docs/building/secrets/)
- [Component Nudging Relationships](https://konflux-ci.dev/docs/building/component-nudges/)
- [Running Tests in Konflux](https://konflux-ci.dev/docs/testing/)
- [Enterprise Contract (Conforma) Compliance](https://konflux-ci.dev/docs/compliance/)
- The different parts of a release
  - [Release Plan Admission](https://konflux-ci.dev/docs/releasing/create-release-plan-admission/)
  - [Release Plan](https://konflux-ci.dev/docs/releasing/create-release-plan/)
  - [Release](https://konflux-ci.dev/docs/releasing/create-release/)
- [Building an OLM Operator](https://konflux-ci.dev/docs/end-to-end/building-olm/)

## Tenant and Application Structure

- **Tenant namespace**: `logical-volume-manag-tenant`
- **Konflux applications**: `lvm-operator-5-0` (operator, bundle, must-gather)
  and `lvm-operator-catalog-5-0` (catalog)
- **Service accounts**: one per component (e.g. `build-pipeline-lvm-operator-5-0`)

## `.tekton/` Directory Layout

```text
.tekton/
├── multi-arch-build-pipeline.yaml              # Pipeline: multi-arch builds (operator, must-gather)
├── single-arch-build-pipeline.yaml             # Pipeline: single-arch builds (bundle)
├── catalog-patching-build-pipeline.yaml        # Pipeline: FBC build with pre/post patching (catalog)
├── lvm-operator-5-0-push.yaml                  # PipelineRun: operator on push to main
├── lvm-operator-5-0-pull-request.yaml          # PipelineRun: operator on PR to main
├── lvm-operator-bundle-5-0-push.yaml           # PipelineRun: bundle on push to main
├── lvm-operator-bundle-5-0-pull-request.yaml   # PipelineRun: bundle on PR to main
├── lvm-operator-catalog-5-0-push.yaml          # PipelineRun: catalog on push to main
├── lvm-operator-catalog-5-0-pull-request.yaml  # PipelineRun: catalog on PR to main
├── lvms-must-gather-5-0-push.yaml              # PipelineRun: must-gather on push to main
├── lvms-must-gather-5-0-pull-request.yaml      # PipelineRun: must-gather on PR to main
├── lvm-operator-integration-tests.yaml         # Pipeline: unit test runner for Konflux ITs
└── images-mirror-set.yaml                      # ImageDigestMirrorSet for FIPS/pullspec resolution
```

There are three kinds of resources:

- **Pipeline definitions** (3): reusable pipeline templates
- **PipelineRun definitions** (8): per-component, per-event triggers that
  Pipelines as Code instantiates
- **Supporting config** (2): the integration test pipeline and the IDMS

### `.tekton/images-mirror-set.yaml`

An `ImageDigestMirrorSet` that maps production image references to their
Konflux tenant and staging equivalents:

| Production (source)                              | Mirrors                                  |
| ------------------------------------------------ | ---------------------------------------- |
| `registry.redhat.io/lvms4/lvms-rhel9-operator`   | `quay.io/.../lvm-operator`, `staging/…`  |
| `registry.redhat.io/lvms4/lvms-operator-bundle`  | `quay.io/.../lvm-operator-bundle`, `…`   |
| `registry.redhat.io/lvms4/lvms-must-gather-rhel9`| `quay.io/.../lvms-must-gather`, `…`      |

Used for two purposes:

1. **FIPS compliance checks**: The `fbc-fips-check-oci-ta` task uses it to
   resolve production image references to their CI equivalents
2. **OPM pullspec rewriting**: The `run-opm-command` task uses it (via
   `idms_path`) to substitute production references in catalog JSON during
   builds

### Pipeline Trigger Model

Each PipelineRun uses a [Pipelines as Code](https://pipelinesascode.com/)
CEL expression to fire only when relevant files change on the target branch.

The pattern for the current in-development version is:

```yaml
event == "<push|pull_request>" && target_branch == "main" && (<pathChanged expressions>)
```

The pattern for publicly released versions is:

```yaml
event == "<push|pull_request>" && target_branch == "release-<x.y version>" && (<pathChanged expressions>)
```

### PR builds vs push builds

| Aspect           | Pull Request           | Push to main                                     |
| ---------------- | ---------------------- | ------------------------------------------------ |
| Image tag        | `on-pr-{{revision}}`   | `{{revision}}`                                   |
| Image expiry     | 5 days                 | No expiry                                        |
| Additional tags  | Skipped                | Applied (from `konflux.additional-tags` label)   |
| Security checks  | All enabled            | All enabled                                      |
| Max kept runs    | 3                      | 3                                                |

### Remote Pipeline References

All of the build pipelines across all versions of the operator reference the
pipelines from `main` via
[remote build pipelines](https://konflux-ci.dev/docs/patterns/keep-remote-pipelines-up-to-date/).
This approach solves a few historical issues encountered when onboarding to
Konflux:

1. Mintmaker task update flood — When mintmaker tasks were updated, it caused
   PRs for each component across all versions of the operator to get triggered
   resulting in the build cluster getting overloaded. This historically happened
   multiple times per day or week and the resolution was manually retriggering
   the build pipelines one by one to ensure the cluster didn't get overloaded.
   You also had to hope that another mintmaker update didn't trigger while you
   were trying to manually push through the previous updates.
2. Pipeline maintenance reduction — During onboarding to Konflux it became
   quickly apparent that any time a pipeline needed to be modified or changed
   (outside of mintmaker updates), it was tedious to flow those changes to all
   versions of the operator. By centralizing the pipelines in main, we no
   longer need to go and backport pipeline updates to all of the release
   branches.

> *Note:* a caveat to having the remote pipelines is when an update occurs,
> only the builds from `main` get retriggered. The release branches would
> have to be manually retriggered to receive the updates. This primarily
> becomes important if we are releasing a component that has not been built
> in a long time and the tekton tasks have expired. This can lead to a
> Conforma policy violation and a failure in the Conforma step at release
> time.

## `release/` Directory

### Directory Layout

```text
release/
├── container-build.args                        # Shared build args for all Dockerfiles
├── konflux.make                                # Makefile targets for Konflux operations
├── .gitignore
├── operator/
│   ├── konflux.Dockerfile                      # LVM Operator (operand) Dockerfile
│   ├── rpms.in.yaml                            # Cachi2 RPM dependency spec
│   └── rpms.lock.yaml                          # Generated RPM lockfile (all 4 arches)
├── bundle/
│   └── bundle.konflux.Dockerfile               # OLM bundle Dockerfile
├── catalog/
│   ├── catalog.konflux.Dockerfile              # FBC catalog Dockerfile
│   ├── lvm-operator-catalog-template.yaml      # Released bundle entries (auto-generated)
│   └── lvm-operator-catalog-candidate-template.yaml  # Pre-release entries
├── must-gather/
│   └── must-gather.konflux.Dockerfile          # LVM Operator must-gather Dockerfile
└── hack/
    ├── render_templates.sh                     # Bundle manifest generation
    ├── prepare-catalog.sh                      # Catalog template merge (pre-OPM step)
    ├── render-catalog.sh                       # Catalog JSON patching (post-OPM step)
    ├── generate_catalog_template.sh            # Regenerate catalog templates
    ├── generate-rpm-lock.sh                    # Regenerate rpms.lock.yaml via RHSM
    ├── generate-rpm-lock.md                    # RPM lock generation instructions
    └── update-konflux-task-refs.sh             # Update pinned tekton task bundle digests
```

### `container-build.args`

The single source of truth for version metadata, consumed by all Dockerfiles
via buildah's `--build-arg-file`:

```text
OPERATOR_VERSION=5.0.0
LVMS_TAGS=v5.0
OPENSHIFT_VERSIONS=v5.0-v5.1
MAINTAINER=Your Name <yname@redhat.com>
```

### RPM Dependency Management

The operator image installs system RPMs (`util-linux`, `xfsprogs`,
`e2fsprogs`) which must be pre-fetched for hermetic builds.

- **`rpms.in.yaml`**: Declares required packages, architectures
  (`aarch64`, `x86_64`, `s390x`, `ppc64le`), RHEL 9 repos, and the base
  image pattern
- **`rpms.lock.yaml`**: Generated lockfile containing every RPM (and its
  source RPM) with exact URLs, checksums, and versions for all architectures
- **`generate-rpm-lock.sh`**: Regeneration script that runs inside a UBI 9
  container with RHSM credentials, using `rpm-lockfile-prototype`

The lockfile is consumed by Cachi2's `prefetch-dependencies` task via the
`prefetch-input` parameter:
`[{"type": "rpm", "path": "release/operator"}, {"type": "gomod", "path": "."}]`.

To regenerate:

```bash
export RHSM_ACTIVATION_KEY=<key>
export RHSM_ORG=<org>
make rpm-lock
```

### `konflux.make`

There are several Make targets specifically for Konflux. These are pulled
into the top level LVM Operator `Makefile` and should be run from the
repository root.

| Target                     | Description                                                |
| -------------------------- | ---------------------------------------------------------- |
| `rpm-lock`                 | Regenerate `rpms.lock.yaml` (requires RHSM credentials)    |
| `konflux-update`           | Update pinned task bundle digests in all pipeline YAMLs    |
| `catalog-template`         | Regenerate catalog templates from staging/production       |
| `catalog-source`           | Render catalog JSON via `opm alpha render-template semver` |
| `validate-renovate-config` | Validate `renovate.json` using the mintmaker image         |
| `catalog-container`        | Build the catalog image locally                            |

## Automated Dependency Updates (Renovate/Mintmaker)

The `renovate.json` configuration drives automated updates via Konflux's
Mintmaker for `main` and all `release-x.y` branches:

- **Tekton task bundles**: digest updates in `.tekton/**` and `release/**`
  are auto-merged with `approved` + `lgtm` labels
- **RPM lockfiles**: refreshed automatically via the
  `refresh-rpm-lockfiles` preset
- **Catalog template digests**: custom regex manager tracks digest pins in
  `*-catalog-template.yaml` files against quay.io images
- **Go module minor updates**: disabled (only patch/lockfile maintenance)
- **Lock file maintenance**: scheduled nightly before 5am, auto-merged
- All commits use `NO-JIRA:` prefix and include `Signed-off-by`

## Security and Compliance

All builds include these security measures:

- **FIPS compliance**: Operator compiled with `CGO_ENABLED=1`,
  `GOEXPERIMENT=strictfipsruntime`, `-tags strictfipsruntime`
- **Hermetic builds**: No network access during container builds; all
  dependencies pre-fetched by Cachi2
- **RPM pinning**: `rpms.lock.yaml` pins exact package versions and
  SHA-256 checksums for all architectures
- **Vulnerability scanning**: Clair (per platform), ClamAV, Snyk SAST,
  Coverity SAST (conditional on availability)
- **Code quality**: Shell check, unicode check (trojan source detection)
- **RPM signature verification**: Validates RPM signatures in the built
  image
- **Source images**: SRPM/source images built for license compliance
- **SBOM**: Software Bill of Materials generated and displayed
- **Supply chain provenance**: Tekton Chains records git URL and commit
  for attestation
- **Task bundle pinning**: All task references use digest-pinned bundles
  for reproducibility
- **FBC validation**: Catalog images validated with `opm validate` and
  checked for FIPS compliance and index pruning

## Image Registry Map

| Stage       | Registry                                                    | Example                              |
| ----------- | ----------------------------------------------------------- | ------------------------------------ |
| Development | `quay.io/lvms_dev/`                                         | `.../lvms-operator:latest`           |
| CI builds   | `quay.io/redhat-user-workloads/logical-volume-manag-tenant/`| `.../lvm-operator:{{revision}}`      |
| Staging     | `registry.stage.redhat.io/lvms4/`                           | `.../lvms-rhel9-operator`            |
| Production  | `registry.redhat.io/lvms4/`                                 | `.../lvms-rhel9-operator`            |

## Development vs Production Dockerfiles

The repo contains parallel Dockerfile sets for development and production:

| Purpose     | Development                           | Production (Konflux)                                       |
| ----------- | ------------------------------------- | ---------------------------------------------------------- |
| Operator    | `Dockerfile` (golang + fedora)        | `release/operator/konflux.Dockerfile` (RHEL + UBI)         |
| Bundle      | `bundle.Dockerfile` (scratch)         | `release/bundle/bundle.konflux.Dockerfile` (3-stage)       |
| Catalog     | `catalog.Dockerfile` (upstream opm)   | `release/catalog/catalog.konflux.Dockerfile` (RH registry) |
| Must-Gather | `must-gather/Dockerfile` (origin-cli) | `release/must-gather/must-gather.konflux.Dockerfile`       |

Development Dockerfiles use public base images (golang, fedora, upstream
OPM) and are used for local builds and testing. Production Dockerfiles use
RHEL-based images from `brew.registry.redhat.io` and `registry.redhat.io`,
include FIPS support, full Red Hat labels, and are the only ones used by the
Konflux build system.
