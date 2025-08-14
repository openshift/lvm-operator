# LVM Operator Release Management

## Creating a Release

The process for releasing the **LVM Operator** is as follows:
1. Get QE Approval for the operator and catalog snapshots
2. Prepare the Operator release yaml file
3. Update the release note description in the operator release yaml
3. Open a PR and get docs and QE approvals for the release yaml
4. After merging, the release yaml file can be applied to the konflux cluster in our tenant namespace

**After successful operator release**, the process for releasing the **LVM Operator Catalog** is as follows:
1. Test that the image can be installed using the catalog from the catalog snapshot
    > NOTE: When testing the catalog during the release process, **DO NOT** use an image digest mirror.
2. Prepare the catalog release yaml file
3. Open a PR and get QE approval for the release yaml
4. Apply the docs approved tag to the yaml (we don't need docs approval for the catalog)
5. After merging, the release yaml file can be applied to the konflux cluster in our tenant namespace

### Required Release Information
In order to generate a release you will need to acquire the following information:
- Release specific Jira information
- Release specific CVE information
- The operator snapshot to be released
- The catalog snapshot that relates to the operator snapshot

#### Acquiring Jira/CVE Information for Operator Release
Jira Query:
```
(project = "OpenShift Edge Enablement" AND type = Epic AND component = "Logical Volume Manager Storage" AND fixVersion = openshift-4.19.z) OR (project = "OpenShift Bugs" AND component = "Logical Volume Manager Storage" AND "Target Version" = 4.19.z AND NOT (status = Closed AND (resolution = "Not a Bug" OR resolution = "Duplicate" OR resolution = Done-Errata)) AND status != New)
```

Running the above query (after updating the version numbers to match what you are trying to release) will generate a list of issues to be included in the release. The query can be run here: [Red Hat Issues](https://issues.redhat.com/issues/?jql=)

Once the query has been run and the list of issues is verified, you can export the list as XML, save the XML file and set the `JIRA_XML` environment variable to be the path to the XML file.

> **TODO**: The above query does not account for CVEs, we need to update it to pull in any CVEs that apply to the LVM operator and be included in the XML output

#### Operator Snapshots
The **operator snapshot** should be provided by QE. This will be the snapshot that they verified in preparation for the release. If you are trying to determine what snapshot you should provide QE for testing, you can use the konflux UI to inspect staging release artifacts (the artifacts should have the correct version tag) or run the following in your command line (with your KUBECONFIG set to the konflux kubeconfig):

```bash
$ oc get releases --sort-by=.metadata.creationTimestamp | grep lvm-operator-staging-releaseplan-4-20
lvm-operator-4-20-7z424-3e0fa9a-5wfhb           lvm-operator-4-20-7z424           lvm-operator-stage-releaseplan-4-20           Succeeded        14d
...
lvm-operator-4-20-vtnlq-901cb5c-zz4rw           lvm-operator-4-20-vtnlq           lvm-operator-stage-releaseplan-4-20           Succeeded        2d8h

$ oc get release lvm-operator-4-20-vtnlq-901cb5c-zz4rw -o yaml | yq '{"snapshot": .spec.snapshot, "artifacts": [(.status.artifacts.filtered_snapshot | from_json | .components[] | with_entries(select(.key == "name" or .key == "tags")))]}'
```

These commands will list the staging releases for a given y-stream and then you can inspect a specific release to get the snapshot and version tags for each component. The version tags should match the operator version you are trying to release.

#### Catalog Snapshots
The **catalog snapshot** should also be provided by QE. This will relate to the operator snapshot using the bundle manifest. The SHA value in the operator snapshot should be present in the version stable channel of the catalog referenced by the catalog snapshot.

### Creating the Release yaml file
We have some scripts to make creating the release yaml file easy:
```bash
$ export JIRA_XML=/path/to/the/release/jiras.xml
$ RELEASE_SNAPSHOT="lvm-operator-4-20-abcdef" make release
```

The script will automatically detect if this is an operator release or a catalog release and populate the release yaml accordingly.
The release yaml will be output in the releases folder and can be committed to the repository.

## Release Testing
### Dev Environment Configuration
#### Test Cluster Kubeconfig

The kubeconfig for the cluster used for testing should be located at `${HOME/.kube/config}` and have an `admin` context

#### Konflux Cluster Kubeconfig
You will need to add a kubeconfig for the konflux cluster in `${HOME}/.kube/konflux.kubeconfig` and be defined like the following code block:

<details>

```yaml
apiVersion: v1
clusters:
- cluster:
    server: https://api-toolchain-host-operator.apps.stone-prd-host1.wdlc.p1.openshiftapps.com//workspaces/logical-volume-manag
  name: konfluxInformation
contexts:
- context:
    cluster: konflux
    namespace: logical-volume-manag-tenant
    user: oidc
  name: konflux
current-context: konflux
kind: Config
preferences: {}
users:
- name: oidc
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      args:
      - oidc-login
      - get-token
      - --oidc-issuer-url=https://sso.redhat.com/auth/realms/redhat-external
      - --oidc-client-id=rhoas-cli-prod
      command: kubectl
      env: null
      interactiveMode: IfAvailable
      provideClusterInfo: false
```
</details>

#### Pull Secret
You will need to have your pull secret saved to `${HOME}/.docker/config.json` with credentials defined for the following repos:
- `registry.redhat.io`
- `registry.stage.redhat.io` - This is optional
- `quay.io`

#### CLI Tools
- The Openshift CLI (`oc`)
- `yq` - [Download](https://github.com/mikefarah/yq/?tab=readme-ov-file#install)
- GNU make

#### Environment Variables
| Variable | Required | Example | Description |
| --- | --- | --- | --- |
| `CANDIDATE_VERSION` | **required** | `4.19` | The version of the operator under test (exclude the `v` in the version number) |
| `CATALOG_SOURCE` | *not required* | `lvm-operator-catalogsource` | The catalog source name for the catalog source that will be injected into the test cluster |
| `CLUSTER_OS` | *not required* | `rhel9` | For specifying the operator index |
| `TEST_SNAPSHOT` | *not required* | `lvm-operator-4-19-xkw2b` | For specifying the operator snapshot you want to test |
| `CATALOG_SNAPSHOT` | *not required* | `lvm-operator-catalog-4-19-fq2mp` | For specifying the catalog that contains the artifacts from `TEST_SNAPSHOT` |

> **NOTE** - `CATALOG_SNAPSHOT` **is required** if you specify `TEST_SNAPSHOT`

### Cluster Configuration
Once your dev environment is configured, you can configure the test cluster by running the following commands:

1. Once per test cluster you need to run `make cluster-config`
    - This will update the cluster pull secret and apply the `ImageDigestMirrorSet` required for testing
2. Identify and apply the catalog that will be used for testing to the test cluster
    - `CANDIDATE_VERSION=4.19 make cluster-catalog-config`

    OR

    - If you don't want to test the latest staging release and instead want to test an earlier release:

      `CANDIDATE_VERSION=4.19 TEST_SNAPSHOT='lvm-operator-4-19-abcde' CATALOG_SNAPSHOT='lvm-operator-catalog-4-19-abcde' make cluster-catalog-config`
3. Install the operator
    - `CANDIDATE_VERSION=4.19 make install-operator`
