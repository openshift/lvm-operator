FROM golang:1.24 AS builder

WORKDIR /go/src/github.com/openshift/lvm-operator
COPY . .

WORKDIR /go/src/github.com/openshift/lvm-operator/test/integration

RUN go mod download

RUN GO_COMPLIANCE_POLICY="exempt_all" CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /tmp/integration-test .

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /

COPY --from=builder /tmp/integration-test .

ENTRYPOINT ["./integration-test"]
