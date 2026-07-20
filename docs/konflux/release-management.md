# LVM Operator Release Management

## Release Flows

Konflux uses `ReleasePlan` resources to define how images flow from CI to staging and production. Each y-stream version has two release plans per application:

- **Staging** (`auto-release: true`): Triggered automatically when a snapshot passes integration tests and Enterprise Contract policy. Pushes images to `registry.stage.redhat.io`.
- **Production** (`auto-release: false`): Triggered manually by applying a `Release` CR to the Konflux cluster. Pushes images to `registry.redhat.io` via an advisory (errata).

The operator and catalog are released sequentially — the operator must be released first, then the catalog (which references the released operator bundle).

## Production Releases

Production releases are managed from the `release-management` branch of the `openshift/lvm-operator` repository. This branch is separate from the source code branches and contains release tooling, release YAML manifests, and test cluster configuration scripts.

### Release Process

**Operator release:**

1. QE verifies a staging snapshot
2. Run `RELEASE_SNAPSHOT="<snapshot>" make release` on the `release-management` branch to generate the `Release` YAML
3. Populate Jira/CVE information (pass `JIRA_XML=/path/to/export.xml` to auto-parse)
4. Update the release notes description in the generated YAML
5. Open a PR — requires docs and QE approval
6. After merge, apply the YAML to the Konflux cluster (automated via the `manage-release.sh` postsubmit job)

**Catalog release (after operator release succeeds):**

1. Verify the catalog snapshot installs correctly using the staging catalog image
2. Run `RELEASE_SNAPSHOT="<catalog-snapshot>" make release` to generate the catalog `Release` YAML
3. Open a PR — requires QE approval (docs approval tag is applied without review)
4. After merge, apply to the Konflux cluster

### Release YAML Structure

**Operator releases** (`releases/lvm-operator-rhba-v{x.y.z}.yaml`):

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: Release
metadata:
  name: lvm-operator-rhba-v{x.y.z}
  namespace: logical-volume-manag-tenant
  labels:
      release.appstudio.openshift.io/author: "Your Name <yname@redhat.com>"
spec:
  releasePlan: lvm-operator-production-releaseplan-{x-y}
  snapshot: lvm-operator-{x-y}-{snapshot-id}
  data:
    releaseNotes:
      description: "..."
      issues:
        fixed:
          - id: OCPSTRAT/OCPBUGS/OCPEDGE-1234
            source: redhat.atlassian.net
```

**Catalog releases** (`releases/lvm-operator-catalog-v{x.y.z}.yaml`):

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: Release
metadata:
  name: lvm-operator-catalog-{x-y}-{snapshot-id}
  namespace: logical-volume-manag-tenant
  labels:
      release.appstudio.openshift.io/author: "Your Name <yname@redhat.com>"
spec:
  releasePlan: lvm-operator-catalog-production-releaseplan-{x-y}
  snapshot: lvm-operator-catalog-{x-y}-{snapshot-id}
```

Catalog releases have no `data` section — they carry no advisory or release notes since the catalog is a delivery mechanism, not a user-facing artifact. The released catalog gets merged into the official OpenShift operator catalog.

### Release Automation (`manage-release.sh`)

The `manage-release.sh` script runs as a Prow postsubmit job. When a PR merging a `Release` YAML is merged to the `release-management` branch, the script:

1. Detects which files under `releases/` changed in the merge commit
2. Logs into the Konflux cluster (`stone-prd-rh01`) using a service account token
3. Applies each changed `Release` YAML via `oc apply`

This means merging the PR is the release trigger — no manual `oc apply` is needed.

### Konflux Tenant Configuration (konflux-release-data)

The tenant's Konflux resources (applications, components, release plans, Enterprise Contract policies, integration tests) are defined in the `konflux-release-data` repository from gitlab under `tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/`. This configuration uses `ProjectDevelopmentStream` templates to generate per-version resources:

- **`lvm-operator/`**: Operator application — components, integration tests, Enterprise Contracts, and release plans (staging + production) per y-stream
- **`lvm-operator-catalog/`**: Catalog application — separate components, release plans, and Enterprise Contracts per y-stream
- **Version files** (e.g. `versions/v5-0.yaml`): Instantiate all templates for a specific version, binding the git branch, version number, and template names

Each version gets its own `Application`, `Component`, `IntegrationTestScenario`, `EnterpriseContractPolicy`, and `ReleasePlan` resources automatically generated from the templates.
