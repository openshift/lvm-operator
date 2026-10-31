FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel8:v4.12 as operator-sdk
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_8_1.20 as builder

# The pinned digests below are based on the images shipped in the most recent
# released LVMS 4.12 z-stream (v4.12.9), except the RBAC proxy digest was updated
# to a current image because the v4.11-era digest was removed from the registry.
# The original digests were taken from that bundle's
# ClusterServiceVersion .spec.relatedImages, which records registry.redhat.io
# digests. Mintmaker/Renovate keeps them current once the component is onboarded.
ARG IMG=registry.redhat.io/lvms4/lvms-rhel8-operator@sha256:1e06883ffee5f3a6d6ec6678711791170764a86a57b8b1898557639ca909a688

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel8@sha256:12ae31fef18fc100ce052cb211701527cd723d47078100540b69b2da8cafbe28

ARG TOPOLVM_CSI_IMAGE=registry.redhat.io/lvms4/topolvm-rhel8@sha256:760739dfdefeba24f2e88e16ba355c19b64b765df1e6badc9bb2b021871c19a3

ARG CSI_REGISTRAR_IMAGE=registry.redhat.io/openshift4/ose-csi-node-driver-registrar@sha256:ab467503a868169551107021e8555d437ec12f9bd394ac46ca8e2dbb378f7bd6

ARG CSI_LIVENESSPROBE_IMAGE=registry.redhat.io/openshift4/ose-csi-livenessprobe@sha256:8981c776862be39be33bd9ca61a76cfca5d02d9469d3c8f90c1c0451592c6063

ARG CSI_RESIZER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-resizer@sha256:6816b40254770846598e22a16645fd969421ca4669e36ccf53fa4d198eb1fa09

ARG CSI_PROVISIONER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-provisioner@sha256:a6cb66bcc7bc97b72bfd0483196c53def6e7f33a0e24750b4e57e1076e985be5

# NOTE: on 4.12 the snapshotter ships as the -rhel8 suffixed repository, unlike
# 4.14+ which uses the unsuffixed registry.redhat.io/openshift4/ose-csi-external-snapshotter.
ARG CSI_SNAPSHOTTER_IMAGE=registry.redhat.io/openshift4/ose-csi-external-snapshotter-rhel8@sha256:116987000c38d947ddd7c093ca2880e74aa006bae0149e637452c8c596706d37

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
