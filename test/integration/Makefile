.PHONY: integration-build integration-docker-build integration-run integration-run-sno integration-run-mno integration-clean integration-docker-run all

# Variables
BINARY = integration-test
IMAGE = lvms_integration_test
TAG = latest
FULL_IMAGE = $(IMAGE):$(TAG)

# Default targe
all: integration-build

# Build Go binary for integration tests
integration-build:
	GO_COMPLIANCE_POLICY="exempt_all" CGO_ENABLED=0 go build -a -installsuffix cgo -o $(BINARY) .

# Build Docker image for integration tests
integration-docker-build:
	cd ../.. && docker build -t $(FULL_IMAGE) -f test/integration/Dockerfile .

# Run all integration tests (SNO + MNO)
integration-run: integration-build
	./$(BINARY) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/sno
	./$(BINARY) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/mno

# Run only SNO tests
integration-run-sno: integration-build
	./$(BINARY) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/sno

# Run only MNO tests
integration-run-mno: integration-build
	./$(BINARY) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/mno

# Run integration tests in Docker container
integration-docker-run: integration-docker-build
	docker run --rm $(FULL_IMAGE) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/sno
	docker run --rm $(FULL_IMAGE) run-suite -c 1 openshift/lvm-operator/test/integration/qe_tests/mno


# Clean up local binary and Docker image
integration-clean:
	rm -f $(BINARY)
	docker rmi -f $(FULL_IMAGE) || true
