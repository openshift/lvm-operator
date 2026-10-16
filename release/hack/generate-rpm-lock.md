# Generating rpms.lock.yaml
In order to generate the rpms.lock.yaml file you will need the following:
- podman
- docker auth config for registry.redhat.io
- Red Hat Subscription Manager Org ID and Key
  - Available at https://console.redhat.com/insights/connector/activation-keys
- environment variables:
  - `RHSM_ORG` - this is your Org ID for Red Hat Subscription Manager
  - `RHSM_ACTIVATION_KEY` - this is your activation key for Red Hat Subscription Manager
  - `AUTH_FILE` - (optional) this is the path to your docker auth config.json. Common locations are:
    - [Default] `$XDG_RUNTIME_DIR/containers/auth.json` for podman on linux
    - `~/.docker/config.json` for docker setups
    - Some users set `$REGISTRY_AUTH_FILE` (for podman, skopeo, etc.)
  - `TARGET` - (optional) this is the component you wish to generate a lockfile for. Possible values are:
    - `operator`
    - `must-gather`

The `make -f release/konflux.make rpm-lock` command will start up a container, activate with RHSM and use `rpm-lockfile-prototype` with `rpms.in.yaml` as an input. We are generating for several architectures so it will take a few minutes for the command to finish.

```bash
# Export your envs
export RHSM_ORG="<YOUR_RHSM_ORG>"
export RHSM_ACTIVATION_KEY="<YOUR_RHSM_ACTIVATION_KEY>"
export AUTH_FILE="/path/to/your/docker/config.json"

# From lvm-operator repo root
make -f release/konflux.make rpm-lock TARGET=operator
make -f release/konflux.make rpm-lock TARGET=must-gather
```

The helper runs as root in UBI 9. The inputs select RHEL 9 BaseOS and
AppStream packages for all four architectures. The must-gather input also
includes the OpenShift 4.15 client repository for `openshift-clients`.

After a new bundle reaches staging, refresh the catalog templates:

```bash
make -f release/konflux.make catalog-template Y_STREAM=v4.15
```

Set `SKIP_CANDIDATES=1` to include only released bundles.
