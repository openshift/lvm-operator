# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
# Build the manager binary
FROM golang:1.21 as builder

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

FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:9.4-1134

# We use CentOS Stream 9 as our source for e2fsprogs here so that we can offer a fully open source version for development here.
# This allows users without Red Hat Subscriptions (e.g. on a Fedora Workstation) to build and test LVMS.
# Note that we do NOT provide Support for any images built from this Dockerfile. The authoritative source for LVMS is the
# official Red Hat Container Catalog at https://catalog.redhat.com/software/containers/search?gs&q=lvms4%20operator
# which are built with Red Hat builds of the e2fsprogs package which is only available via Red Hat Subscription.
RUN curl https://mirror.stream.centos.org/9-stream/BaseOS/$(arch)/os/Packages/centos-gpg-keys-9.0-23.el9.noarch.rpm > centos-gpg-keys-9.0-23.el9.noarch.rpm && \
    rpm -i centos-gpg-keys-9.0-23.el9.noarch.rpm && \
    rm -f centos-gpg-keys-9.0-23.el9.noarch.rpm
RUN curl https://mirror.stream.centos.org/9-stream/BaseOS/$(arch)/os/Packages/centos-stream-repos-9.0-23.el9.noarch.rpm > centos-stream-repos-9.0-23.el9.noarch.rpm && \
    rpm -i centos-stream-repos-9.0-23.el9.noarch.rpm && \
    rm -f centos-stream-repos-9.0-23.el9.noarch.rpm

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
