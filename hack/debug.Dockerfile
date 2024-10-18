# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
FROM golang:1.22 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY ../go.mod go.mod
COPY ../go.sum go.sum

# since we use vendoring we don't need to redownload our dependencies every time. Instead we can simply
# reuse our vendored directory and verify everything is good. If not we can abort here and ask for a revendor.
COPY ../vendor vendor/
RUN go mod verify

# Copy the go source
COPY ../api api/
COPY ../cmd cmd/
COPY ../internal internal/

ENV GOARCH=$TARGETARCH
ENV GOOS=$TARGETOS
ENV CGO_ENABLED=0

# Build
RUN go build -gcflags "all=-N -l" -mod=vendor -a -o lvms cmd/main.go

FROM golang:1.22 as dlv
RUN go install -ldflags "-s -w -extldflags '-static'" github.com/go-delve/delve/cmd/dlv@latest

# vgmanager needs 'nsenter' and other basic linux utils to correctly function
FROM --platform=$TARGETPLATFORM registry.ci.openshift.org/ocp/builder:rhel-9-base-openshift-4.17

RUN dnf update -y && \
    dnf install --nodocs --noplugins -y \
        util-linux \
        e2fsprogs \
        xfsprogs \
        glibc && \
    dnf clean all

WORKDIR /app

COPY --from=builder /workspace/lvms /usr/sbin/lvms
COPY --from=dlv /go/bin/dlv /usr/sbin/dlv

USER 65532:65532

EXPOSE 2345

ENTRYPOINT ["/usr/sbin/dlv"]
