# Global args to be used to build the final base image url
ARG CATALOG_VERSION
ARG BASE_IMAGE=registry.redhat.io/openshift4/ose-operator-registry-rhel9

# The builder image is expected to contain
# /bin/opm (with serve subcommand)
FROM quay.io/operator-framework/opm:latest as builder
ARG CATALOG_VERSION
# Copy the FBC into the image at /configs/lvms-operator/catalog.json and pre-populate serve cache
COPY "catalogs/lvm-operator-catalog-${CATALOG_VERSION}.json" /configs/lvms-operator/catalog.json

RUN ["/bin/opm", "validate", "/configs/lvms-operator"]

RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

ARG BASE_IMAGE
ARG CATALOG_VERSION
FROM ${BASE_IMAGE}:${CATALOG_VERSION}
ARG CATALOG_VERSION
# The base image is expected to contain
# /bin/opm (with serve subcommand) and /bin/grpc_health_probe

# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]

COPY --from=builder /configs /configs
COPY --from=builder /tmp/cache /tmp/cache

# Set FBC-specific label for the location of the FBC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs
LABEL konflux.additional-tags="${CATALOG_VERSION}"
