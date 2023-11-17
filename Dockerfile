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

FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:9.3

RUN curl https://mirror.stream.centos.org/9-stream/BaseOS/$(arch)/os/Packages/centos-gpg-keys-9.0-23.el9.noarch.rpm > centos-gpg-keys-9.0-23.el9.noarch.rpm && \
    rpm -i centos-gpg-keys-9.0-23.el9.noarch.rpm && \
    rm -f centos-gpg-keys-9.0-23.el9.noarch.rpm
RUN curl https://mirror.stream.centos.org/9-stream/BaseOS/$(arch)/os/Packages/centos-stream-repos-9.0-23.el9.noarch.rpm > centos-stream-repos-9.0-23.el9.noarch.rpm && \
    rpm -i centos-stream-repos-9.0-23.el9.noarch.rpm && \
    rm -f centos-stream-repos-9.0-23.el9.noarch.rpm

RUN microdnf update -y && microdnf install -y util-linux e2fsprogs xfsprogs glibc && microdnf clean all

WORKDIR /
COPY --from=builder /workspace/lvms .
USER 65532:65532

# '/lvms' is the entrypoint for all LVMS binaries
ENTRYPOINT ["/lvms"]
