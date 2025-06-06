#!/bin/bash

oc="${OC:-oc}"

cluster_pull_secret="$(${oc} get secret/pull-secret -n openshift-config --template='{{index .data ".dockerconfigjson" | base64decode}}')"

mkdir -p manifests

jq -s '.[0] * .[1]' <(echo "${cluster_pull_secret}") <(cat $HOME/.docker/config.json) > manifests/pull-secret.json

oc set data secret/pull-secret -n openshift-config --from-file=.dockerconfigjson=manifests/pull-secret.json

rm -f manifests/pull-secret.json
