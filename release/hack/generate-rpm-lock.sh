#!/bin/bash
set -euo pipefail

# Disable container mode
rm /etc/rhsm-host

# Register the container with RHSM
subscription-manager clean
subscription-manager register --activationkey="${RHSM_ACTIVATION_KEY}" --org="${RHSM_ORG}"

target="${1}"

# Install tools from UBI 9. Target RHEL 8 repos come from rpms.in.yaml.
dnf install -y  python3 python3-pip python3-dnf skopeo rpm
python3 -m pip install https://github.com/konflux-ci/rpm-lockfile-prototype/archive/refs/heads/main.zip

cd release

# Get the SSL Keys
keydir="/etc/pki/entitlement"
keyfile=$(ls $keydir | grep "\-key")
export DNF_VAR_SSL_CLIENT_KEY="${keydir}/${keyfile}"
export DNF_VAR_SSL_CLIENT_CERT="${keydir}/${keyfile//-key}"

# Generate the rpms.lock.yaml file for the operator
rpm-lockfile-prototype --outfile="${target}/rpms.lock.yaml" ${target}/rpms.in.yaml
