FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4

# Copy all collection scripts to /usr/bin
COPY must-gather/collection-scripts /usr/bin/

RUN mkdir /licenses
COPY LICENSE /licenses

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

USER 65532:65532

ENTRYPOINT ["/usr/bin/gather"]
