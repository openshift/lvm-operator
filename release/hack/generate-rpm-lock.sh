#!/bin/bash
set -x

# Register the container with RHSM
subscription-manager clean
subscription-manager register --activationkey="${RHSM_ACTIVATION_KEY}" --org="${RHSM_ORG}"

arch=$(uname -m)
target="${1}"

# Activate the repos
dnf config-manager \
    --enable rhel-9-for-${arch}-appstream-rpms \
    --enable rhel-9-for-${arch}-appstream-source-rpms \
    --enable rhel-9-for-${arch}-baseos-rpms \
    --enable rhel-9-for-${arch}-baseos-source-rpms

# Install pip, skopeo and rpm-lockfile-prototype
dnf install -y pip skopeo
pip install https://github.com/konflux-ci/rpm-lockfile-prototype/archive/refs/tags/v0.18.0.tar.gz

cd release

# Get the SSL Keys
keydir="/etc/pki/entitlement"
keyfile=$(ls $keydir | grep "\-key")
export DNF_VAR_SSL_CLIENT_KEY="${keydir}/${keyfile}"
export DNF_VAR_SSL_CLIENT_CERT="${keydir}/${keyfile//-key}"

# Generate the rpms.lock.yaml file for the operator
rpm-lockfile-prototype --outfile="${target}/rpms.lock.yaml" ${target}/rpms.in.yaml
