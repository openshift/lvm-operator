FROM golang:1.24 AS builder

# Set working directory inside the builder container
WORKDIR /workspace

# Copy the integration test source code and go.mod
COPY test/integration/ ./

# Download all dependencies and verify
RUN go mod download

# Build the binary
RUN GO_COMPLIANCE_POLICY="exempt_all" CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o integration-test .

# ---- Stage 2: Runtime container ----
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /

# Copy the built binary from the builder stage
COPY --from=builder /workspace/integration-test .

# Run the binary
ENTRYPOINT ["./integration-test"]
