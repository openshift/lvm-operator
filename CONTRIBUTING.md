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

### Installing `pre-commit`

For our local development, we supply a `pre-commit` configuration that can be used to verify common issues before
submitting them to a pipeline (which is a common prerogative called shift-left). You can follow the installation instructions at https://pre-commit.com/#installation.
pre-commit hooks contain all important verifications that are also checked by our pipelines, and you will be able to easily use a commit hook that way.

After installing `pre-commit`, navigate to the repository root and run `pre-commit install`. Now, whenever you commit, all pre-commit checks will be executed for you.
Also, you can run `pre-commit run` to run the check with all currently staged files.

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

#### Remotely debugging LVMS inside a cluster

A typical issue for any workload interacting with nodes in kubernetes is that it is hard to test properly.
This is because nodes usually have their own specific environment and can be hard to debug.
During development, you can still remotely debug into both `lvm-operator` and `vgmanager` by attaching remotely to the debugger.

For this you need 2 things:
1. An image made specifically to include a debugging server and debugging symbols for stack trace information.
   Run `make docker-build-debug` to build one for you.
2. A deployment that starts the operator through the debugging server.
   We have the [`debug`](config/debug) kustomize target for this.
   Run `deploy-debug` after building the image to run the debugger for you.

Now we can remotely attach to the binaries in the cluster on port `2345`.
However, we first need to port-forward into the cluster:
1. Run `oc port-forward deploy/lvms-operator 2345:2345` to port-forward to the controller.
2. Run `oc port-forward pod/vgmanager-xxx 2345:2345` to port-forward to a vgmanager pod on a node.

After opening the port, you will only need to connect to the debugger and set breakpoints.
Here are some tutorials on remotely connecting to a running binary:

- [Visual Studio Code](https://github.com/golang/vscode-go/blob/master/docs/debugging.md#connect-to-headless-delve-with-target-specified-at-server-start-up)
- [Goland](https://www.jetbrains.com/help/go/attach-to-running-go-processes-with-debugger.html#step-3-create-the-remote-run-debug-configuration-on-the-client-computer)

### Executing Performance Tests against LVMS

1. set up a running OpenShift cluster and install LVMS either via CSV or locally as described above.
2. login via username & password, ideally kubeadmin, and get the token via `oc whoami -t` to later inject into the test suite. OIDC logins dont use tokens and cannot be used.
2. download go and install it on your machine (https://golang.org/doc/install) if not already done
3. use `go run ./test/performance --help` to see the available options for performance testing.

Mainly we differentiate between 2 kinds of performance tests: Load/Stress Testing, and Idle testing.
This is important to differentiate because for most installations on edge, LVMS will not be actively working all the time.
This is because after volume groups and thin pools are created, they will not be touched frequently.
Thus we need to test the performance of LVMS in an idle state (when volumes are created already) and in a load/stress state (when volumes are created and deleted frequently).

Note that to properly get information on LVMS performance, we require the Openshift cluster to be running with monitoring enabled.
We query a prometheus route for the commonly installed prometheus-k8s instance in Openshift and use it to aggregate performance metrics in quantiles.
To actually access prometheus, we need to supply the performance test suite a token with the `-t` flag.

#### Stress Testing

For Stress testing, one should create a new LVMCluster and corresponding StorageClass in advance, e.g.:
```shell
oc apply -f ./config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage
```

One can run Stress Tests by running the performance tests like so:
```shell
go run ./test/performance -t $(oc whoami -t) -s lvms-vg1 -i 64
```

where `-s` is used to pass the generated storage class and `-i` represents the PVC/Pod combination amount to be used for stress testing.
A higher iteration count will lead to longer stress testing times but also more accurate results.

#### Idle Testing

One can run Idle Tests by slightly adjusting the test execution:
```shell
go run ./test/performance -t $(oc whoami -t) --run-stress false --long-term-observation-window=5m
```

This will run the test suite for 5 minutes and observe the performance of LVMS in an idle state.
Due to `--run-stress=false` no PVCs will be created and deleted, but the test suite will still observe the performance of LVMS in an idle state from the prometheus instance.
Ideally the system should run for long periods of time in advance and then have a long observation window for most accurate quantile reports.

#### Performance Test report

The test report will be generated in the working directory of the performance tests and will be a `.toml` file with broken down metrics on Pod and Container level.
We include 90/95/99 Quantiles for Memory and CPU as our main metrics. Other metrics are not included in the tool and have to be manually fetched via prometheus.

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
