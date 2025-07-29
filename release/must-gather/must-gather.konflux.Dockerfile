FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4

ARG MAINTAINER
ARG OPERATOR_VERSION
ARG LVMS_TAGS

RUN microdnf update -y && \
    microdnf install -y --nodocs --setopt=install_weak_deps=0 tar rsync findutils gzip iproute tcpdump pciutils util-linux nftables procps-ng openshift-clients && \
    microdnf clean all && \
    rm -rf /var/cache/*

# Copy all collection scripts to /usr/bin
COPY must-gather/collection-scripts /usr/bin/

RUN mkdir /licenses
COPY LICENSE /licenses

LABEL maintainer="${MAINTAINER}"
LABEL com.redhat.component="lvms-must-gather-container"
LABEL name="lvms4/lvms-must-gather-rhel9"
LABEL version="${OPERATOR_VERSION}"
LABEL description="LVM Storage data gathering image"
LABEL summary="LVM Storage data gathering image"
LABEL io.k8s.display-name="LVM Storage must gather"
LABEL io.k8s.description="LVM Storage data gathering image"
LABEL io.openshift.tags="lvms"
LABEL upstream-vcs-ref="${CI_LVM_OPERATOR_UPSTREAM_COMMIT}"
LABEL konflux.additional-tags="${LVMS_TAGS} v${OPERATOR_VERSION}"
LABEL test="test"

USER 65532:65532

ENTRYPOINT ["/usr/bin/gather"]
