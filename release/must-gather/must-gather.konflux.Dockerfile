FROM registry.redhat.io/openshift4/ose-cli-rhel9:latest

# Copy all collection scripts to /usr/bin
COPY must-gather/collection-scripts /usr/bin/

LABEL maintainer="Suleyman Akbas <sakbas@redhat.com>"
LABEL com.redhat.component="lvms-must-gather-container"
LABEL name="lvms4/lvms-must-gather-rhel9"
LABEL version="${CI_CONTAINER_VERSION}"
LABEL description="LVM Storage data gathering image"
LABEL summary="LVM Storage data gathering image"
LABEL io.k8s.display-name="LVM Storage must gather"
LABEL io.k8s.description="LVM Storage data gathering image"
LABEL io.openshift.tags="lvms"
LABEL upstream-vcs-ref="${CI_LVM_OPERATOR_UPSTREAM_COMMIT}"

ENTRYPOINT ["/usr/bin/gather"]
