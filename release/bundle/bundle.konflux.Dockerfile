FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel8:v4.14 as operator-sdk
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.21 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel9-operator@sha256:0b3200e9c19859e6b8befc1a60f306914ff25f4e95c8bb148f8d40547878ccb8

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel9@sha256:8fc206411f44f258564285b099b243057621cfedcc890a7c1c819550a4e561d9

ARG TOPOLVM_CSI_IMAGE=registry.redhat.io/lvms4/topolvm-rhel9@sha256:856b01e888274f1fc824e6b0a69c2bcf6b467407d3fe0f70ea07ceaf3c926594

ARG CSI_REGISTRAR_IMAGE=registry.redhat.io/openshift4/ose-csi-node-driver-registrar@sha256:2e7eac2a2c52f7c6f7ec5a1b2a9c1a211e56be6233d6b1fc5a29bb7d05f26fae

ARG CSI_LIVENESSPROBE_IMAGE=registry.redhat.io/openshift4/ose-csi-livenessprobe@sha256:0b1a832cfe80115e575006eff8ba530173feb4b5a09894f39fcaec74f34301f9

ARG CSI_RESIZER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-resizer@sha256:80e0f91e57a762a30d94c447a53804b2da2b4615d06ac6270c742b9992dc0cd1

ARG CSI_PROVISIONER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-provisioner@sha256:17863070837ed7675c3973e970e674048510cfc8e577463d66600da7d6498f50

ARG CSI_SNAPSHOTTER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-snapshotter@sha256:5d874f411747733394335aa2193b7fe495074d17bbddc0e324a040b202de3166

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


RUN mkdir bin && \
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
