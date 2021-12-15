FROM quay.io/openshift/origin-cli:latest

# copy all collection scripts to /usr/bin
COPY collection-scripts /usr/bin/

ENTRYPOINT /usr/bin/gather
