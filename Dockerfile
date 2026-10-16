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

RUN go mod download && go mod verify

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# Build
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly --ldflags "-s -w" -a -o lvms cmd/main.go

# vgmanager needs 'nsenter' and other basic linux utils to correctly function
FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:9.2

# Update the image to get the latest CVE updates
RUN microdnf update -y && \
    microdnf install -y util-linux && \
    microdnf clean all

WORKDIR /
COPY --from=builder /workspace/lvms .
USER 65532:65532

# '/lvms' is the entrypoint for all LVMS binaries
ENTRYPOINT ["/lvms"]
