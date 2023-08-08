# How to Contribute

The lvm-operator project is under the [Apache 2.0 license](LICENSE). We accept contributions via GitHub pull requests. This document outlines how to contribute to the project.

## Contribution Flow

Developers must follow these steps to make a change:

1. Fork the `openshift/lvm-operator` repository on GitHub.
2. Create a branch from the `main` branch, or from a versioned branch (such
   as `release-4.13`) if you are proposing a backport.
3. Make changes.
4. Create tests as needed and ensure that all tests pass.
5. Push your changes to a branch in your fork of the repository.
6. Submit a pull request to the `openshift/lvm-operator` repository.
7. Work with the community to make any necessary changes through the code
   review process (effectively repeating steps 3-7 as needed).

## Developer Environment Installation

### Cluster builds
In order to build on the cluster you need to first have your kubeconfig configured. Once configured you can run the following steps to build on the cluster:

#### Configure the build

```bash
$ make create-buildconfig
```

This will build from `https://github.com/openshift/lvm-operator` on branch `main` by default. This can be overridden by specifying the `GIT_URL` and `GIT_BRANCH` environment variables.
```bash
$ GIT_URL=https://github.com/my-user/lvm-operator.git \
GIT_BRANCH=my-feature-branch \
make create-buildconfig
```

#### Run the build
Kickoff the build on the cluster. All output will be followed for the build.
```bash
$ make cluster-build
```

#### Deploy the operator
To deploy the built operator run the following command:
```bash
$ make cluster-deploy
```

To undeploy the operator you can run
```bash
$ make undeploy
```


### Local E2E Testing

1. Download OpenShift Local from https://developers.redhat.com/products/openshift-local/overview
2. `crc setup` (once per machine)
3. `crc start`
4. ```shell
   credentials=$(crc console --credentials -o json)
   oc login -u $(echo $credentials | jq -r ".clusterConfig.adminCredentials.username") \
    -p $(echo $credentials | jq -r ".clusterConfig.adminCredentials.password") \
    $(echo $credentials | jq -r ".clusterConfig.url")
   ```
5. `oc config view --raw >> /tmp/crc-kubeconfig`
6. `export KUBECONFIG="/tmp/crc-kubeconfig"`
7. `make deploy`
8. `make e2e`

#### Enable Snapshot Testing

Prerequisites: Ensure you have a running CRC Cluster (Step 6)

1. Make sure controller is undeployed with `make undeploy`
2. `oc apply -k https://github.com/kubernetes-csi/external-snapshotter//client/config/crd`
3. `oc apply -k https://github.com/kubernetes-csi/external-snapshotter//deploy/kubernetes/snapshot-controller`
4. Start again at Step 7

## Commits Per Pull Request

Pull requests should always represent a complete logical change. Where possible, pull requests should be composed of multiple commits that each make small but meaningful changes. Striking a balance between minimal commits and logically complete changes is an art as much as a science, but when it is possible and reasonable, divide your pull request into more commits.

It makes sense to separate work into individual commits for changes such as:
- Changes to unrelated formatting and typo fixes.
- Refactoring changes that prepare the codebase for your logical change.

When breaking down commits, each commit should leave the codebase in a working state. The code should add necessary unit tests where required, and pass unit tests, formatting tests, and usually functional tests. There can be times when exceptions to these requirements are appropriate. For instance, it is sometimes useful for maintainability to split code changes and related changes to CRDs and CSVs. Unless you are very sure this is true for your change, make sure that each commit passes CI checks as above.

Make sure to update the bundle manifests after making changes:

```bash
make bundle
```

## Commit structure

LVM Operator maintainers value clear and explanatory commit messages. By default, each of your commits must follow the rules below:

### We follow the common commit conventions
```
type: subject

body?

footer?
```

### Here is an example of an acceptable commit message for a bug fix:
```
component: commit title

This is the commit message, where I'm explaining what the bug was, along
with its root cause.
Then I'm explaining how I fixed it.

Fix: https://bugzilla.redhat.com/show_bug.cgi?id=<NUMBER>

Signed-off-by: First_Name Last_Name <email address>
```

### Here is an example of an acceptable commit message for a new feature:
```
component: commit title

This is the commit message, here I'm explaining, what this feature is
and why do we need it.

Signed-off-by: First_Name Last_Name <email address>
```

### More Guidelines:
- Type/component should not be empty.
- Your commit message should not exceed more than 72 characters per line.
- Header should not have a full stop.
- Body should always end with the full stop.
- There should be one blank line between header and body.
- There should be one blank line between body and footer.
- Your commit must be signed-off.
- *Recommendation*: A "Co-authored-by:" line should be added for each
  additional author.
