# LVM Operator Release Management

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
  name: konflux
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

### Cluster Configuration
Once your dev environment is configured, you can configure the test cluster by running the following commands:

1. Once per test cluster you need to run `make cluster-config`
    - This will update the cluster pull secret and apply the `ImageDigestMirrorSet` required for testing
2. Identify and apply the catalog that will be used for testing to the test cluster
    - `CANDIDATE_VERSION=4.19 make cluster-catalog-config`
3. Install the operator
    - `CANDIDATE_VERSION=4.19 make install-operator`
