FROM golang:1.23 AS builder

# Copy the entire repo to preserve directory structure for go.mod replace directives
WORKDIR /go/src/github.com/openshift/lvm-operator
COPY . .

# Move into the integration test directory
WORKDIR /go/src/github.com/openshift/lvm-operator/test/integration

# Download all dependencies and verify
RUN go mod download

# Build the binary
RUN GO_COMPLIANCE_POLICY="exempt_all" CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /tmp/integration-test .

# ---- Stage 2: Runtime container ----
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /

# Copy the built binary from the builder stage
COPY --from=builder /tmp/integration-test .

# Run the binary
ENTRYPOINT ["./integration-test"]
