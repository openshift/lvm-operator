# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
# Build the manager binary
FROM golang:1.22 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

COPY deps deps/
RUN go mod download
RUN go mod verify

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# Build
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build --ldflags "-s -w" -a -o lvms cmd/main.go

FROM --platform=$TARGETPLATFORM fedora:latest

RUN dnf update -y && \
    dnf install --nodocs --noplugins -y \
        util-linux \
        e2fsprogs \
        xfsprogs \
        glibc && \
    dnf clean all

WORKDIR /
COPY --from=builder /workspace/lvms .
USER 65532:65532

# '/lvms' is the entrypoint for all LVMS binaries
ENTRYPOINT ["/lvms"]
