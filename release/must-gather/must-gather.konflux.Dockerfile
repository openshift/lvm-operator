FROM registry.redhat.io/openshift4/ose-must-gather-rhel9:v4.20@sha256:a1376ed9e13b8ff171b967369a9ecf2dd0cf1188f34d456bc475023a86c87292

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
