FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.23 as builder

ARG IMG=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator@sha256:dcc58d02bc9b52d2e2933d4beae8f9ef95ff7d5e10e88c6e0d4bc1794e47c821

ARG LVM_MUST_GATHER=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather@sha256:39f3f0394d16717832da99cd3ca1fab3cc4e67a3e208f6b115a71108ef26b6e1


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
