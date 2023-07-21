#!/bin/bash

set -euo pipefail

GIT_URL="${GIT_URL:-}"
if [ -z "$GIT_URL" ]; then GIT_URL=$(git config --get remote.origin.url); fi

GIT_BRANCH="${GIT_BRANCH:-}"
if [ -z "$GIT_BRANCH" ]; then GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD); fi

echo "Configuring build from branch ${GIT_BRANCH} in repo ${GIT_URL}"

oc apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    workload.openshift.io/allowed: "management"
  labels:
    app.kubernetes.io/name: lvms-operator
    security.openshift.io/scc.podSecurityLabelSync: "false"
    pod-security.kubernetes.io/enforce: "privileged"
    pod-security.kubernetes.io/warn: "privileged"
    pod-security.kubernetes.io/audit: "privileged"
    openshift.io/cluster-monitoring: "true"
  name: openshift-storage

---

apiVersion: image.openshift.io/v1
kind: ImageStream
metadata:
  name: lvms-operator
  namespace: openshift-storage

---

apiVersion: build.openshift.io/v1
kind: BuildConfig
metadata:
  name: lvms-operator
  namespace: openshift-storage
spec:
  output:
    to:
      kind: ImageStreamTag
      name: lvms-operator:latest
  source:
    git:
      uri: ${GIT_URL}
      ref: ${GIT_BRANCH}
    type: Git
  strategy:
    dockerStrategy:
      dockerfilePath: Dockerfile
    type: Docker
EOF