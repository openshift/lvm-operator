.PHONY: integration-build integration-docker-build integration-run integration-clean integration-docker-run integration-help all

# Variables
BINARY = integration-test
IMAGE = lvms_integration_test
TAG = latest
FULL_IMAGE = $(IMAGE):$(TAG)

# Default targe
all: integration-build

# Build Go binary for integration tests
integration-build:
	go build -o $(BINARY) .

# Build Docker image for integration tests
integration-docker-build:
	docker build -t $(FULL_IMAGE) .

# Run integration tests locally (binary)
integration-run: integration-build
	./$(BINARY)

# Run integration tests in Docker container
integration-docker-run: integration-docker-build
	docker run --rm $(FULL_IMAGE)


# Clean up local binary and Docker image
integration-clean:
	rm -f $(BINARY)
	docker rmi -f $(FULL_IMAGE) || true
