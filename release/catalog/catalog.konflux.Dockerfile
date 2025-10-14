ARG CATALOG_VERSION
ARG BASE_IMAGE=registry.redhat.io/openshift4/ose-operator-registry-rhel9
FROM ${BASE_IMAGE}:${CATALOG_VERSION}
ARG CATALOG_VERSION

COPY release/catalog/lvm-operator-catalog.json /configs/lvms-operator/catalog.json

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
