# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
FROM golang:1.20 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY ../go.mod go.mod
COPY ../go.sum go.sum

RUN go mod download && go mod verify

# Copy the go source
COPY ../api api/
COPY ../cmd cmd/
COPY ../internal internal/

ENV GOARCH=$TARGETARCH
ENV GOOS=$TARGETOS
ENV CGO_ENABLED=0

# Build
RUN go build -gcflags "all=-N -l" -mod=readonly -a -o lvms cmd/main.go

FROM golang:1.20 as dlv
RUN go install -ldflags "-s -w -extldflags '-static'" github.com/go-delve/delve/cmd/dlv@latest

# vgmanager needs 'nsenter' and other basic linux utils to correctly function
FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:9.2

# Update the image to get the latest CVE updates
RUN microdnf update -y && \
    microdnf install -y util-linux && \
    microdnf clean all

WORKDIR /app

COPY --from=builder /workspace/lvms /usr/sbin/lvms
COPY --from=dlv /go/bin/dlv /usr/sbin/dlv

USER 65532:65532

EXPOSE 2345

ENTRYPOINT ["/usr/sbin/dlv"]
