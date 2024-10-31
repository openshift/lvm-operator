#!/bin/bash

set -eu

REF="v0.6.0"
NS="openshift-storage"

while getopts "r:n:" OPT; do
    case $OPT in
        r)
            REF="$OPTARG"
            ;;
        n)
            NS="$OPTARG"
            ;;
        ?|*)
            exit 1
            ;;
    esac
done

tmp_dir=$(mktemp -d)

cat <<EOF > "${tmp_dir}/kustomization.yaml"
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: "$NS"

resources:
- https://gitlab.com/kubesan/kubesan/deploy/openshift?ref=${REF}
EOF

oc apply -k "$tmp_dir"