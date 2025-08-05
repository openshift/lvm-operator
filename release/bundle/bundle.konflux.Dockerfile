FROM registry.redhat.io/openshift4/ose-operator-sdk-rhel9:v4.18 as operator-sdk
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel9-operator@sha256:1c98c923280c5d94b30642006e63b5f1af3f0c7e78e694c15dfef6a0270c5ff6

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel9@sha256:965ff56a90838d4e810bdf65c2a76cd7404dc51ab9c7a0c9be1060dd8579d83c

ARG OPERATOR_VERSION

WORKDIR /operator
COPY ./ ./

ENV GOFLAGS="-mod=readonly"
ENV GOBIN=/operator/bin

RUN mkdir bin && \
    go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen && \
    go install -mod=readonly sigs.k8s.io/kustomize/kustomize/v5

COPY --from=operator-sdk /usr/local/bin/operator-sdk ./bin/operator-sdk

RUN CI_VERSION=${OPERATOR_VERSION} IMG=${IMG} LVM_MUST_GATHER=${LVM_MUST_GATHER} ./release/hack/render_templates.sh

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
LABEL konflux.additional-tags="${LVMS_TAGS} v${OPERATOR_VERSION}"
