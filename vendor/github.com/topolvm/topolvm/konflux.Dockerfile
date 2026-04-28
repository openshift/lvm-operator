# Build Stage 1
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder

ARG TOPOLVM_VERSION

WORKDIR /workdir
COPY . /workdir

RUN go version | tee -a /go.version

ENV CGO_ENABLED=1
ENV GOOS=linux
ENV GOEXPERIMENT=strictfipsruntime

RUN go build -tags strictfipsruntime -o build/hypertopolvm -mod=mod -ldflags "-w -s -X github.com/topolvm/topolvm.Version=${TOPOLVM_VERSION}" ./cmd/hypertopolvm

# Build Stage 2
FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4

# Update the image to get the latest CVE updates
RUN microdnf update -y && \
    microdnf install -y util-linux xfsprogs e2fsprogs && \
    microdnf clean all

COPY --from=builder /workdir/build/hypertopolvm /hypertopolvm
COPY --from=builder /go.version /go.version

RUN ln -s hypertopolvm /lvmd \
    && ln -s hypertopolvm /topolvm-scheduler \
    && ln -s hypertopolvm /topolvm-node \
    && ln -s hypertopolvm /topolvm-controller

LABEL maintainer="Suleyman Akbas <sakbas@redhat.com>"
LABEL com.redhat.component="topolvm-container"
LABEL name="lvms4/topolvm-rhel9"
LABEL version="${CI_CONTAINER_VERSION}"
LABEL description="LVMS TopoLVM"
LABEL summary="The Topolvm CSI and controller."
LABEL io.k8s.display-name="LVMS TopoLVM"
LABEL io.k8s.description="LVM Storage TopoLVM"
LABEL io.openshift.tags="lvms"
LABEL upstream-vcs-ref="${CI_TOPOLVM_UPSTREAM_COMMIT}"

ENTRYPOINT ["/hypertopolvm"]
