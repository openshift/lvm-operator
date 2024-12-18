#!/bin/bash
set -x

# Register the container with RHSM
subscription-manager register --activationkey="${RHSM_ACTIVATION_KEY}" --org="${RHSM_ORG}"

# Activate the repos
dnf config-manager \
    --enable rhel-9-for-x86_64-appstream-rpms \
    --enable rhel-9-for-x86_64-appstream-source-rpms \
    --enable rhel-9-for-x86_64-baseos-rpms \
    --enable rhel-9-for-x86_64-baseos-source-rpms

# Install pip, skopeo and rpm-lockfile-prototype
dnf install -y pip skopeo
pip install https://github.com/konflux-ci/rpm-lockfile-prototype/archive/refs/tags/v0.13.1.tar.gz

cd build

cp /etc/yum.repos.d/redhat.repo ./redhat.repo

# Overwrite the arch listing so that we can do multiarch
sed -i "s/$(uname -m)/\$basearch/g" ./redhat.repo

# Generate the rpms.lock.yaml file
rpm-lockfile-prototype --allowerasing --outfile="rpms.lock.yaml" rpms.in.yaml

# Cleanup the repo file
rm -rf ./redhat.repo
