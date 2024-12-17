FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.23 as builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM

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

ENV CGO_ENABLED=1
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
ENV GOEXPERIMENT=strictfipsruntime

RUN go build -tags strictfipsruntime -mod=vendor -ldflags "-s -w" -a -o lvms cmd/main.go

FROM --platform=$TARGETPLATFORM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4

RUN microdnf update -y && \
    microdnf install -y util-linux xfsprogs e2fsprogs glibc && \
    microdnf clean all

RUN [ -d /run/lock ] || mkdir /run/lock

WORKDIR /
COPY --from=builder /workspace/lvms .
USER 65532:65532

LABEL maintainer="Suleyman Akbas <sakbas@redhat.com>"
LABEL com.redhat.component="lvms-operator-container"
LABEL name="lvms4/lvms-rhel9-operator"
LABEL version="4.19.0"
LABEL description="LVM Storage Operator"
LABEL summary="Provides the latest LVM Storage Operator package."
LABEL io.k8s.display-name="LVM Storage Operator based on RHEL 9"
LABEL io.k8s.description="LVM Storage Operator container based on Red Hat Enterprise Linux 9 Image"
LABEL io.openshift.tags="lvms"
LABEL lvms.tags="v4.19"

ENTRYPOINT ["/lvms"]
