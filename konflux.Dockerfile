# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG OCP_VERSION=4.16
ARG GOLANG_VERSION=1.21
ARG RHEL_VERSION=9
ARG UBI_VERSION=9.3
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM

# Build the manager binary
FROM registry.ci.openshift.org/ocp/builder:rhel-${RHEL_VERSION}-golang-${GOLANG_VERSION}-openshift-${OCP_VERSION} as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# since we use vendoring we don't need to redownload our dependencies every time. Instead we can simply
# reuse our vendored directory and verify everything is good. If not we can abort here and ask for a revendor.
COPY vendor vendor/
RUN go mod verify

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# Build
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=vendor --ldflags "-s -w" -a -o lvms cmd/main.go

FROM --platform=$TARGETPLATFORM registry.ci.openshift.org/ocp/${OCP_VERSION}:base-rhel${RHEL_VERSION} as baseocp

FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi${RHEL_VERSION}/ubi-minimal:${UBI_VERSION}

RUN microdnf update -y && \
    microdnf install --nodocs --noplugins -y \
        util-linux \
        e2fsprogs \
        xfsprogs \
        glibc && \
    microdnf clean all

WORKDIR /
COPY --from=builder /workspace/lvms .
USER 65532:65532

# '/lvms' is the entrypoint for all LVMS binaries
ENTRYPOINT ["/lvms"]
