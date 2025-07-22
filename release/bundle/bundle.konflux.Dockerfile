FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.23 as builder

ARG IMG=registry.redhat.io/lvms4/lvms-rhel9-operator@sha256:df3dbf591d55689f0979830f7185ef8ce521d7e3e78a85e2c0057cd1b9f1b66d

ARG LVM_MUST_GATHER=registry.redhat.io/lvms4/lvms-must-gather-rhel9@sha256:b641b4741f5e5c373a5276f9ebfa699a6304614be321a3d248272237ba886cdb

ARG OPERATOR_VERSION

WORKDIR /operator
COPY ./ ./

RUN mkdir bin && \
    cp /cachi2/output/deps/generic/* bin/ && \
    tar -xvf bin/kustomize.tar.gz -C bin && \
    chmod +x bin/operator-sdk bin/controller-gen

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
