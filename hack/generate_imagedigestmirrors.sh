#!/bin/bash

os=${CLUSTER_OS:-rhel9}

mkdir -p manifests

cat <<EOF > manifests/imagedigestmirrors.yaml
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: lvm-operator-imagedigestmirrors
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/lvms4/lvms-operator-bundle
    mirrors:
      - registry.stage.redhat.io/lvms4/lvms-operator-bundle
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
  - source: registry.redhat.io/lvms4/lvms-${os}-operator
    mirrors:
      - registry.stage.redhat.io/lvms4/lvms-${os}-operator
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
  - source: registry.redhat.io/lvms4/lvms-must-gather-${os}
    mirrors:
      - registry.stage.redhat.io/lvms4/lvms-must-gather-${os}
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
  - source: registry.stage.redhat.io/lvms4/lvms-operator-bundle
    mirrors:
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle
  - source: registry.stage.redhat.io/lvms4/lvms-${os}-operator
    mirrors:
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator
  - source: registry.stage.redhat.io/lvms4/lvms-must-gather-${os}
    mirrors:
      - quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather
EOF

echo "ImageDigestMirrorSet manifest created at manifests/imagedigestmirrors.yaml"
