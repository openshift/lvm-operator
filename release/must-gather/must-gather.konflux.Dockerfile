FROM registry.redhat.io/openshift4/ose-must-gather-rhel9:v4.16@sha256:11d808f065b099f0d95e3787dc63b15c9bc679a0b855cfd0901d722b1721e9b4

ARG MAINTAINER
ARG OPERATOR_VERSION
ARG LVMS_TAGS

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
LABEL cpe="cpe:/a:redhat:lvms:${LVMS_TAGS#v}::el9"

USER 65532:65532

ENTRYPOINT ["/usr/bin/gather"]
