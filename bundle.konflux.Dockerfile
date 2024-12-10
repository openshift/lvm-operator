FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder
ARG IMG=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvm-operator@sha256:26256fe9c503be536aa63cc6e1797a5d414afd76ed562713f2361b91b105715a
ARG LVM_MUST_GATHER=quay.io/redhat-user-workloads/logical-volume-manag-tenant/lvms-must-gather@sha256:6199d9658ccd6e97e1c2ecdf9771a5233caff2c637411ed207691e02394839d7
WORKDIR /operator
COPY ./ ./
RUN CI_VERSION="4.18.0" IMG=${IMG} LVM_MUST_GATHER=${LVM_MUST_GATHER} ./hack/render_templates.sh

FROM scratch

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
LABEL com.redhat.openshift.versions="v4.18-v4.19"
LABEL com.redhat.delivery.backport=false

# Standard Red Hat labels
LABEL com.redhat.component="lvms-operator-bundle-container"
LABEL name="lvms4/lvms-operator-bundle"
LABEL version="4.18.0"
LABEL release="1"
LABEL summary="An operator bundle for LVM Storage Operator"
LABEL io.k8s.display-name="lvms-operator-bundle"
LABEL maintainer="Suleyman Akbas <sakbas@redhat.com>"
LABEL description="An operator bundle for LVM Storage Operator"
LABEL io.openshift.tags="lvms"
LABEL lvms.tags="v4.18"
