# Logical Volume Manager Storage Operator Hacking

## Cluster builds
In order to build on the cluster you need to first have your kubeconfig configured. Once configured you can run the following steps to build on the cluster:

### Configure the build
    
```bash
$ make create-buildconfig
```
    
This will build from `https://github.com/openshift/lvm-operator` on branch `main` by default. This can be overridden by specifying the `GIT_URL` and `GIT_BRANCH` environment variables.
```bash
$ GIT_URL=https://github.com/my-user/lvm-operator.git \
GIT_BRANCH=my-feature-branch \
make create-buildconfig
```

### Run the build
Kickoff the build on the cluster. All output will be followed for the build.
```bash
$ make cluster-build
```

### Deploy the operator
To deploy the built operator run the following command:
```bash
$ make cluster-deploy
```

To undeploy the operator you can run
```bash
$ make undeploy
```


## Local E2E Testing

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

### Enable Snapshot Testing

Prerequisites: Ensure you have a running CRC Cluster (Step 6)

1. Make sure controller is undeployed with `make undeploy`
2. `oc apply -k https://github.com/kubernetes-csi/external-snapshotter//client/config/crd`
3. `oc apply -k https://github.com/kubernetes-csi/external-snapshotter//deploy/kubernetes/snapshot-controller`
4. Start again at Step 7