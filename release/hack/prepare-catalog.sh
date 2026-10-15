#!/bin/bash

quay_bundle_path="quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator-bundle"
staging_bundle_path="registry.stage.redhat.io/lvms4/lvms-operator-bundle"
candidate_template="./release/catalog/lvm-operator-catalog-candidate-template.yaml"
released_template="./release/catalog/lvm-operator-catalog-template.yaml"

# Update the pre-release catalog references to use Quay.
pre_release_catalog=$(cat "${candidate_template}")
echo "${pre_release_catalog//${staging_bundle_path}/${quay_bundle_path}}" > "${candidate_template}"

# The variables in this expression belong to yq, not the shell.
# shellcheck disable=SC2016
yq --inplace eval-all '. as $item ireduce ({}; . *+ $item)' "${released_template}" "${candidate_template}"
