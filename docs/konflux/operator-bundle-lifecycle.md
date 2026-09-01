# LVM Operator Bundle, Operand, and must-gather

## Dockerfiles

### `release/operator/konflux.Dockerfile`

Two-stage build producing the operator binary:

- **Builder stage**: `brew.registry.redhat.io/rh-osbs/openshift-golang-builder:<rhel_version>_<go_version>` (ex: `rhel_9_1.25`)
  - Compiles with `CGO_ENABLED=1`, `GOEXPERIMENT=strictfipsruntime`, `-tags strictfipsruntime` for FIPS compliance
  - Build command: `go build -tags strictfipsruntime -mod=readonly -ldflags "-s -w" -a -o lvms cmd/main.go`
- **Runtime stage**: `registry.redhat.io/ubi9/ubi-minimal` (pinned by digest)
  - Installs `util-linux`, `xfsprogs`, `e2fsprogs` via microdnf
  - Runs as non-root UID 65532:65532

The `konflux.additional-tags` label controls what extra tags are applied to the image (e.g. `v5.0 v5.0.0`).

### `release/bundle/bundle.konflux.Dockerfile`

Three-stage build producing the OLM operator bundle:

1. **SDK stage**: `registry.redhat.io/openshift4/ose-operator-sdk-rhel9:v4.18` — extracts `operator-sdk`
2. **Builder stage**: `brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.25`
   - Installs `controller-gen` and `kustomize` via `go install`
   - Runs `release/hack/render_templates.sh` to generate bundle manifests with production image references
   - Takes `IMG` (operand digest) and `LVM_MUST_GATHER` (must-gather digest) as build args for `relatedImages`
3. **Final stage**: `FROM scratch` — content-only image containing `manifests/`, `metadata/`, and `tests/scorecard/`

**Older release branches (4.12-4.15)**: Before TopoLVM was compiled into the single operator binary, the bundle Dockerfile accepted additional image ARGs for the TopoLVM CSI driver and its sidecars (`TOPOLVM_CSI_IMAGE`, `CSI_REGISTRAR_IMAGE`, `CSI_LIVENESSPROBE_IMAGE`, `CSI_RESIZER_IMAGE`, `CSI_PROVISIONER_IMAGE`, `CSI_SNAPSHOTTER_IMAGE`, `RBAC_PROXY_IMAGE`). These were all pinned by digest and injected into the CSV's `relatedImages` list. Starting with 4.16, these extra images were removed because the operator binary now includes TopoLVM, and the CSI sidecar images are resolved at runtime from the OpenShift release payload rather than pinned in the bundle.

### `release/must-gather/must-gather.konflux.Dockerfile`

- Base: `registry.redhat.io/openshift4/ose-must-gather-rhel9:v4.20` (pinned by digest)
- Copies collection scripts from `must-gather/collection-scripts/`
- Runs as non-root UID 65532:65532

## Build Pipelines

### `multi-arch-build-pipeline`

Used by the operator and must-gather components. Builds container images for all four architectures using remote builders, then assembles a multi-arch manifest list.

**Task execution flow:**

```text
init
└── clone-repository
    ├── prefetch-dependencies (Cachi2: gomod + rpm)
    └── generate-labels (release=$ACTUAL_DATE)
        └── build-images (matrix: per platform, buildah-remote)
            └── build-image-index (multi-arch manifest)
                ├── build-source-image (SRPM, when enabled)
                ├── deprecated-base-image-check
                ├── clair-scan (matrix: per platform)
                ├── ecosystem-cert-preflight-checks
                ├── sast-snyk-check
                ├── clamav-scan (matrix: per arch)
                ├── coverity-availability-check
                │   └── sast-coverity-check (when coverity available)
                ├── sast-shell-check
                ├── sast-unicode-check
                ├── rpms-signature-scan
                ├── apply-tags
                └── push-dockerfile
[finally]: show-sbom
```

Key characteristics:

- Default platforms: `linux/x86_64`, `linux/arm64`, `linux/ppc64le`, `linux/s390x`
- Uses `buildah-remote-oci-ta` for distributed multi-arch builds.
- Hermetic builds by default (no network during build)
- All task bundles pinned by digest from `quay.io/konflux-ci/tekton-catalog/`
- Exports `IMAGE_URL`, `IMAGE_DIGEST`, `CHAINS-GIT_URL`, `CHAINS-GIT_COMMIT` for Tekton Chains provenance

### `single-arch-build-pipeline`

Used by the OLM bundle component. Similar structure to the multi-arch pipeline but simplified:

- Uses `buildah-oci-ta` (local build) instead of remote builders
- No `build-image-index` task (single architecture)
- No `push-dockerfile` task
- Has `skip-preflight` parameter (bundles skip ecosystem cert checks)
- Includes a `show-summary` finally task alongside `show-sbom`

#### Bundle Manifest Generation

`release/hack/render_templates.sh` runs inside the bundle Dockerfile builder stage:

- Derives the y-stream version from `OPERATOR_VERSION` (e.g. `5.0.0` → `5.0`)
- Sets OLM channel (`stable-5.0`), `skipRange` (`>=4.2.0 <5.0.0`), and `replaces` (for z-stream patches)
- Runs `make bundle-base` to generate manifests via controller-gen and kustomize
- Appends `relatedImages` (operator + must-gather pinned digests) to the CSV
- Substitutes dev image references (`quay.io/lvms_dev/...`) with production digest references

## Integration Tests

The `lvm-operator-integration-tests.yaml` pipeline runs unit tests as a Konflux integration test. It:

1. Reads the source repo URL and commit SHA from pod annotations
2. Installs `git`, `make`, `golang`, and `patch` via dnf at runtime
3. Clones the repository at the specific revision
4. Runs `NON_ROOT=true make test`

This pipeline is triggered by Konflux's integration testing framework after successful component builds, not by Pipelines as Code directly.
