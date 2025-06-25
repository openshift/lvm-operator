#!/bin/bash
set -x

# Disable container mode
rm /etc/rhsm-host

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
dnf install -y  python3 python3-pip python3-dnf skopeo rpm
python3 -m pip install https://github.com/konflux-ci/rpm-lockfile-prototype/archive/refs/heads/main.zip

cd release

cp /etc/yum.repos.d/redhat.repo ./$target/redhat.repo

# Overwrite the arch listing so that we can do multiarch
sed -i "s/$(uname -m)/\$basearch/g" ./$target/redhat.repo

# Generate the rpms.lock.yaml file for the operator
rpm-lockfile-prototype --debug --allowerasing --outfile="${target}/rpms.lock.yaml" ${target}/rpms.in.yaml

# Cleanup the repo file
rm -rf ./$target/redhat.repo
