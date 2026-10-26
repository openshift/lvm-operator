FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel8:v4.15 as operator-sdk
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.21 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel9-operator@sha256:d6132734b815afcb2b24ad3cca2ec1930bf872f64763001ee14f4dbf24838468

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel9@sha256:49e6a26bb1f2b3028f61775a1ea81ab79d71e1423d53c61836c474843506c275

ARG TOPOLVM_CSI_IMAGE=registry.redhat.io/lvms4/topolvm-rhel9@sha256:7400aaf6fa38aecdb4c3e8c13aabe553e740a917d246d427207e7b36fb533947

ARG CSI_REGISTRAR_IMAGE=registry.redhat.io/openshift4/ose-csi-node-driver-registrar@sha256:05dceaff7f0b2b769ddfd9261cc66d42aab9158ce99ed3dbd6c229e784072635
ARG CSI_LIVENESSPROBE_IMAGE=registry.redhat.io/openshift4/ose-csi-livenessprobe@sha256:771d513c4d4e5d77d797b4c18f86e23162a20c6d39955e93d24f62b94e62e785
ARG CSI_RESIZER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-resizer@sha256:82c01635b32821f04a7cfcf80da8b3f7e097786f220e0c85aab47bda4536a1d6
ARG CSI_PROVISIONER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-provisioner@sha256:2db4f32824a8f9f6780dc3d236f73684cb20db41466ffa8f9a89fd3f5eb62fdd
ARG CSI_SNAPSHOTTER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-snapshotter-rhel9@sha256:479454d0d4de6609d8a4696b9c7f0097115afb45db3bdd46631d58afd77dcc58
ARG RBAC_PROXY_IMAGE=registry.redhat.io/openshift4/ose-kube-rbac-proxy@sha256:7ce50398cd29df6e07ccd89790a53a711a7e5b484698c48f6581f5d4e9efb05a
ARG OPERATOR_VERSION

ENV CI_VERSION="${OPERATOR_VERSION}"
ENV IMG="${IMG}"
ENV LVM_MUST_GATHER="${LVM_MUST_GATHER}"
ENV TOPOLVM_CSI_IMAGE="${TOPOLVM_CSI_IMAGE}"
ENV CSI_REGISTRAR_IMAGE="${CSI_REGISTRAR_IMAGE}"
ENV CSI_LIVENESSPROBE_IMAGE="${CSI_LIVENESSPROBE_IMAGE}"
ENV CSI_RESIZER_IMAGE="${CSI_RESIZER_IMAGE}"
ENV CSI_PROVISIONER_IMAGE="${CSI_PROVISIONER_IMAGE}"
ENV CSI_SNAPSHOTTER_IMAGE="${CSI_SNAPSHOTTER_IMAGE}"
ENV RBAC_PROXY_IMAGE="${RBAC_PROXY_IMAGE}"

WORKDIR /operator
COPY ./ ./

ENV GOFLAGS="-mod=readonly"
ENV GOBIN=/operator/bin

RUN mkdir bin && \
    go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen && \
    go install -mod=readonly sigs.k8s.io/kustomize/kustomize/v5

COPY --from=operator-sdk /usr/local/bin/operator-sdk ./bin/operator-sdk

RUN ./release/hack/render_templates.sh

FROM scratch

ARG MAINTAINER
ARG OPERATOR_VERSION
ARG OPENSHIFT_VERSIONS
ARG LVMS_TAGS

# Copy files to locations specified by labels.
COPY --from=builder /operator/bundle/manifests /manifests/
COPY --from=builder /operator/bundle/metadata /metadata/
COPY --from=builder /operator/bundle/tests/scorecard /tests/scorecard/

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=lvms-operator

# Operator bundle metadata
LABEL com.redhat.delivery.operator.bundle=true
LABEL com.redhat.openshift.versions="${OPENSHIFT_VERSIONS}"
LABEL com.redhat.delivery.backport=false

# Standard Red Hat labels
LABEL com.redhat.component="lvms-operator-bundle-container"
LABEL name="lvms4/lvms-operator-bundle"
LABEL version="${OPERATOR_VERSION}"
LABEL release="1"
LABEL summary="An operator bundle for LVM Storage Operator"
LABEL io.k8s.display-name="lvms-operator-bundle"
LABEL maintainer="${MAINTAINER}"
LABEL description="An operator bundle for LVM Storage Operator"
LABEL io.k8s.description="An operator bundle for LVM Storage Operator"
LABEL url="https://github.com/openshift/lvm-operator"
LABEL vendor="Red Hat, Inc."
LABEL io.openshift.tags="lvms"
LABEL lvms.tags="${LVMS_TAGS}"
LABEL distribution-scope="public"

# Additional labels for the bundle for use in the release process
LABEL konflux.additional-tags="${LVMS_TAGS} v${OPERATOR_VERSION}"
LABEL cpe="cpe:/a:redhat:lvms:${LVMS_TAGS#v}::el9"
