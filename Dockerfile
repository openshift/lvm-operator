# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
# Build the manager binary
FROM golang:1.20 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build --ldflags "-s -w" -a -o manager main.go
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build --ldflags "-s -w" -a -o vgmanager cmd/vgmanager/main.go

# vgmanager needs 'nsenter' and other basic linux utils to correctly function
FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:9.2

# Update the image to get the latest CVE updates
RUN microdnf update -y && \
    microdnf install -y openssl && \
    microdnf install -y util-linux && \
    microdnf clean all

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/vgmanager .
USER 65532:65532

# '/manager' is lvm-operator entrypoint
ENTRYPOINT ["/manager"]

# '/vgmanager' is vgmanager entrypoint which is used in daemonset image
# ENTRYPOINT ["/vgmanager"]
