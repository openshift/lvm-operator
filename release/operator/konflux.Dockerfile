FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_8_1.20 as builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# release-4.12 vendors its dependencies, so the vendor tree is copied in and the
# build runs with -mod=vendor. This keeps the build hermetic without relying on
# module downloads at build time (4.14+ dropped vendor/ and builds -mod=readonly).
COPY vendor/ vendor/

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY controllers/ controllers/
COPY monitoring/ monitoring/
COPY main.go main.go

ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
ENV GOEXPERIMENT=strictfipsruntime
ENV GOFLAGS="-mod=vendor"

RUN CGO_ENABLED=0 GOOS=linux go build --ldflags "-s -w" -a -o manager main.go
RUN CGO_ENABLED=0 GOOS=linux go build --ldflags "-s -w" -a -o vgmanager cmd/vgmanager/main.go
RUN CGO_ENABLED=0 GOOS=linux go build --ldflags "-s -w" -a -o metricsexporter cmd/metricsexporter/exporter.go

FROM --platform=$TARGETPLATFORM registry.redhat.io/ubi8/ubi-minimal@sha256:8de6d48ae7b421bca02fc402543eb69b2acb3dcadf18c0287d509eeea08f6fb2

ARG MAINTAINER
ARG OPERATOR_VERSION
ARG LVMS_TAGS

RUN microdnf update -y && \
    microdnf install -y util-linux xfsprogs e2fsprogs && \
    microdnf clean all

RUN [ -d /run/lock ] || mkdir /run/lock

WORKDIR /
COPY --from=builder /workspace/manager /workspace/vgmanager /workspace/metricsexporter /

RUN mkdir /licenses
COPY LICENSE /licenses

USER 65532:65532

LABEL maintainer="${MAINTAINER}"
LABEL com.redhat.component="lvms-operator-container"
LABEL name="lvms4/lvms-rhel8-operator"
LABEL version="${OPERATOR_VERSION}"
LABEL description="LVM Storage Operator"
LABEL summary="Provides the latest LVM Storage Operator package."
LABEL io.k8s.display-name="LVM Storage Operator based on RHEL 8"
LABEL io.k8s.description="LVM Storage Operator container based on Red Hat Enterprise Linux 8 Image"
LABEL io.openshift.tags="lvms"
LABEL lvms.tags="${LVMS_TAGS}"
LABEL konflux.additional-tags="${LVMS_TAGS} v${OPERATOR_VERSION}"
LABEL cpe="cpe:/a:redhat:lvms:${LVMS_TAGS#v}::el8"

ENTRYPOINT ["/manager"]
