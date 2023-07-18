# Logical Volume Manager Storage Operator Hacking

## Cluster builds
In order to build on the cluster you need to first have your kubeconfig configured. Once configured you can run the following steps to build on the cluster:

### Configure the build
    
```bash
$ make create-buildconfig
```
    
This will build from `https://github.com/openshift/lvm-operator` on branch `main` by default. This can be overridden by specifying the `GIT_REPO` and `GIT_BRANCH` environment variables.
```bash
$ GIT_REPO=https://github.com/my-user/lvm-operator.git \
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