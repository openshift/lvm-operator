FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel8:v4.13 as operator-sdk
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_8_1.21 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel8-operator@sha256:6587e8cdde6d474352939ec09e1d0ec91d6994852b5d2d5bd13d0750d1db86b5

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel8@sha256:8d0285c761eb67ccf9e1c4f2cc615fcad06fb46b38ed14db1421a38622cee19f

ARG TOPOLVM_CSI_IMAGE=registry.redhat.io/lvms4/topolvm-rhel8@sha256:0eb6de6c283dd084639fe80f4d25213bb0a3c83f760c9e3395509ce60a17638b

ARG CSI_REGISTRAR_IMAGE=registry.redhat.io/openshift4/ose-csi-node-driver-registrar@sha256:44120dccc70961d1a2421a553cf87ebbfa937d1c1ef8d774d130831c77c61334

ARG CSI_LIVENESSPROBE_IMAGE=registry.redhat.io/openshift4/ose-csi-livenessprobe@sha256:3f2f4a41c266131a04237c80a3f5107d714f1db37b7e184f71a50ea01551ff68

ARG CSI_RESIZER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-resizer@sha256:f91911474f8b24b0f5b32627e503caccc6f17628ae4e1c649ada6b25b73cc8a4

ARG CSI_PROVISIONER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-provisioner@sha256:ae612a47e9096e0df71633c17f5385bf06800e86602698bb604479da01d6a610

# NOTE: on 4.13 the snapshotter ships as the -rhel8 suffixed repository, unlike
# 4.14+ which uses the unsuffixed registry.redhat.io/openshift4/ose-csi-external-snapshotter.
ARG CSI_SNAPSHOTTER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-snapshotter-rhel8@sha256:40cfc78da681f31e0b3b7769f0a27a15dab50a4b8d888acff2bdd10b345e71a1

ARG RBAC_PROXY_IMAGE=registry.redhat.io/openshift4/ose-kube-rbac-proxy@sha256:6ba961c2c2a29750c0132fe6dd6fa9f6001010afbc5f19b98add87b31b54bcf6

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


RUN mkdir -p bin && \
    go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen && \
    go install -mod=readonly sigs.k8s.io/kustomize/kustomize/v4

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
LABEL cpe="cpe:/a:redhat:lvms:${LVMS_TAGS#v}::el8"
