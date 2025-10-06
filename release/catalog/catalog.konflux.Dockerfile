ARG CATALOG_VERSION
ARG BASE_IMAGE=registry.redhat.io/openshift4/ose-operator-registry-rhel9
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder
# Patch the staging references to be post-release compatible
WORKDIR /catalog
COPY release/catalog/lvm-operator-catalog.json ./release/catalog/lvm-operator-catalog.json
COPY release/hack/render-catalog.sh ./release/hack/render-catalog.sh

RUN ./release/hack/render-catalog.sh

# Global args to be used to build the final base image url
FROM ${BASE_IMAGE}:${CATALOG_VERSION}
ARG CATALOG_VERSION

COPY --from=builder catalog/release/catalog/lvm-operator-catalog.json /configs/lvms-operator/catalog.json

RUN ["/bin/opm", "validate", "/configs/lvms-operator"]

RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

# The base image is expected to contain
# /bin/opm (with serve subcommand) and /bin/grpc_health_prob
# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]

# Set FBC-specific label for the location of the FBC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs
LABEL konflux.additional-tags="${CATALOG_VERSION}"
LABEL version="${CATALOG_VERSION}"
