# Variables
APP_NAME 	:= pgrwl
OUTPUT   	:= $(APP_NAME)
COV_REPORT 	:= coverage.txt
TEST_FLAGS 	:= -v -race -timeout 30s
INSTALL_DIR := /usr/local/bin

ifeq ($(OS),Windows_NT)
	OUTPUT := $(APP_NAME).exe
endif

# Lint the code
.PHONY: lint
lint:
	golangci-lint run --output.tab.path=stdout

# Build the binary
.PHONY: build
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$(OUTPUT) main.go

# Install the binary to /usr/local/bin
.PHONY: install
install: build
	@echo "Installing bin/$(OUTPUT) to $(INSTALL_DIR)..."
	@install -m 0755 bin/$(OUTPUT) $(INSTALL_DIR)

# Run unit tests
.PHONY: test
test:
	go test -v -cover ./...

# Check goreleaser
.PHONY: snapshot
snapshot:
	goreleaser release --skip sign --skip publish --snapshot --clean

# Run tests with coverage
.PHONY: test-cov
test-cov:
	go test -coverprofile=$(COV_REPORT) ./...
	go tool cover -html=$(COV_REPORT)

# Slow tests (that runs inside containers)
.PHONY: test-integ-scripts
test-integ-scripts:
	@currdir=$$(pwd) && cd test/integration/environ && bash run-tests.sh | tee $$currdir/test-integ-scripts.log

.PHONY: image
image:
	docker buildx build -t mailboxsq7/pgrwl .
	docker push mailboxsq7/pgrwl
