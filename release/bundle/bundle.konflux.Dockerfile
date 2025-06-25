FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.21 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel9-operator@sha256:696edeb4c168646b49b8b0ca5b3d06071ab66fdb010e7cd46888d8b2f3a0c7a8
ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel9@sha256:382ceea1151da56d0931403207c9f85721d58a66b39b1cd79790eca6219807d0
ARG TOPOLVM_CSI_IMAGE=registry.redhat.io/lvms4/topolvm-rhel9@sha256:074a206d36c9e4198d90baed86f3046abd2ec12f9b36a715b60900011366b3d4
ARG CSI_REGISTRAR_IMAGE=registry.redhat.io/openshift4/ose-csi-node-driver-registrar@sha256:bafbe772c77a66f48cdd31749e6fb3114125fa428890ba707515d7b74a4f9bdc
ARG CSI_LIVENESSPROBE_IMAGE=registry.redhat.io/openshift4/ose-csi-livenessprobe@sha256:af7c1e9a9e8f2ffc66587e5c0b67dc3fd16c5a6f70e1586fe17170f3cd12df8d
ARG CSI_RESIZER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-resizer@sha256:fd62320124f44d35d136c5c2ce44020f0ccb8b73ba2078fb674a8e06bc3ef369
ARG CSI_PROVISIONER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-provisioner@sha256:d9a205ccf13e127183b9f5d2008f4acba609446a35d7e24a0b261cd35d8d97d7
ARG CSI_SNAPSHOTTER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-snapshotter-rhel9@sha256:8cc7bd58f72d3aa9306e8f27c6730fb8af50b18c2ed5eaf62cbc831cb7bee673
ARG RBAC_PROXY_IMAGE=registry.redhat.io/openshift4/ose-kube-rbac-proxy@sha256:f15d124e4868a4f3ee30141535abc7f2654a3707f8595fd28bbc9ab0d366c9df
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

RUN mkdir bin && \
    cp /cachi2/output/deps/generic/* bin/ && \
    tar -xvf bin/kustomize.tar.gz -C bin && \
    chmod +x bin/operator-sdk bin/controller-gen

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
