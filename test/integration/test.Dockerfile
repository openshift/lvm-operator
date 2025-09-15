FROM golang:1.24 AS builder

# Set working directory inside the builder container
WORKDIR /workspace

# Copy go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary from test/integration
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o integration-test ./test/integration

# ---- Stage 2: Runtime container ----
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Set working directory in runtime container
WORKDIR /

# Copy the built binary from the builder stage
COPY --from=builder /workspace/integration-test .

# Run the binary
ENTRYPOINT ["./integration-test"]

