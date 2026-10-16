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
keyfiles=(/etc/pki/entitlement/*-key.pem)
if [[ ${#keyfiles[@]} -ne 1 || ! -f "${keyfiles[0]}" ]]; then
    echo "Expected one RHSM entitlement key in /etc/pki/entitlement" >&2
    exit 1
fi

export DNF_VAR_SSL_CLIENT_KEY="${keyfiles[0]}"
export DNF_VAR_SSL_CLIENT_CERT="${keyfiles[0]%-key.pem}.pem"

# Generate the rpms.lock.yaml file for the operator
rpm-lockfile-prototype --outfile="${target}/rpms.lock.yaml" "${target}/rpms.in.yaml"
