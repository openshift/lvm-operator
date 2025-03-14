FROM quay.io/openshift/origin-cli:latest

# copy all collection scripts to /usr/bin
COPY collection-scripts /usr/bin/

USER 65532:65532

ENTRYPOINT /usr/bin/gather
