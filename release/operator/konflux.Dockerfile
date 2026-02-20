FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

ENV CGO_ENABLED=1
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
ENV GOEXPERIMENT=strictfipsruntime

RUN go build -tags strictfipsruntime -mod=readonly -ldflags "-s -w" -a -o lvms cmd/main.go

FROM --platform=$TARGETPLATFORM registry.redhat.io/ubi9/ubi-minimal@sha256:c7d44146f826037f6873d99da479299b889473492d3c1ab8af86f08af04ec8a0

ARG MAINTAINER
ARG OPERATOR_VERSION
ARG LVMS_TAGS

RUN microdnf update -y && \
    microdnf install -y util-linux xfsprogs e2fsprogs && \
    microdnf clean all

RUN [ -d /run/lock ] || mkdir /run/lock

WORKDIR /
COPY --from=builder /workspace/lvms .

RUN mkdir /licenses
COPY LICENSE /licenses

USER 65532:65532

LABEL maintainer="${MAINTAINER}"
LABEL com.redhat.component="lvms-operator-container"
LABEL name="lvms4/lvms-rhel9-operator"
LABEL version="${OPERATOR_VERSION}"
LABEL description="LVM Storage Operator"
LABEL summary="Provides the latest LVM Storage Operator package."
LABEL io.k8s.display-name="LVM Storage Operator based on RHEL 9"
LABEL io.k8s.description="LVM Storage Operator container based on Red Hat Enterprise Linux 9 Image"
LABEL io.openshift.tags="lvms"
LABEL lvms.tags="${LVMS_TAGS}"
LABEL konflux.additional-tags="${LVMS_TAGS} v${OPERATOR_VERSION}"
LABEL cpe="cpe:/a:redhat:lvms:${LVMS_TAGS#v}::el9"


ENTRYPOINT ["/lvms"]
