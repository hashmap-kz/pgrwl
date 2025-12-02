# Variables
APP_NAME 	 := pgrwl
OUTPUT   	 := $(APP_NAME)
COV_REPORT 	 := coverage.txt
TEST_FLAGS 	 := -v -race -timeout 30s
INSTALL_DIR  := /usr/local/bin
PROFILE_DIR  := ./pprof
PPROF_SERVER := http://localhost:7070

ifeq ($(OS),Windows_NT)
	OUTPUT := $(APP_NAME).exe
endif

.PHONY: build
build: gen ## Build the binary
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$(OUTPUT) main.go

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run --output.tab.path=stdout

.PHONY: gen
gen: ## Run go generate
	go generate ./...

.PHONY: install
install: build ## Install the binary to $(INSTALL_DIR)
	@echo "Installing bin/$(OUTPUT) to $(INSTALL_DIR)..."
	@install -m 0755 bin/$(OUTPUT) $(INSTALL_DIR)

.PHONY: snapshot
snapshot: ## Run snapshot build with goreleaser
	goreleaser release --skip sign --skip publish --snapshot --clean

.PHONY: test
test: ## Run unit tests
	go test -v -cover ./...

.PHONY: test-cov
test-cov: ## Run tests with coverage report
	go test -coverprofile=$(COV_REPORT) ./...
	go tool cover -html=$(COV_REPORT)

.PHONY: test-integ-scripts-17
test-integ-scripts-17: ## Slow tests (that runs inside containers)
	@currdir=$$(pwd) && cd test/integration/environ && PG_MAJOR=17 bash run-tests.sh | tee $$currdir/test-integ-scripts.log

.PHONY: test-integ-scripts-18
test-integ-scripts-18: ## Slow tests (that runs inside containers)
	@currdir=$$(pwd) && cd test/integration/environ && PG_MAJOR=18 bash run-tests.sh | tee $$currdir/test-integ-scripts.log

.PHONY: image
image: ## Build and push Docker image to localhost:5000
	docker buildx build -t localhost:5000/pgrwl .
	docker push localhost:5000/pgrwl

## Profiling

.PHONY: run
run: build ## Run the binary with local config
	export PGHOST="localhost" && \
	export PGPORT="5432" && \
	export PGUSER="postgres" && \
	export PGPASSWORD="postgres" && \
	bin/$(OUTPUT) daemon -c hack/configs/localfs/receive.yml -m receive

.PHONY: profile-cpu
profile-cpu: ## Capture CPU profile and open web UI
	nohup bash hack/scripts/switch-wals-25.sh &
	go tool pprof -http=: http://localhost:7070/debug/pprof/profile?seconds=20

.PHONY: pprof1
pprof1: ## Collect allocs, heap, CPU, and trace profiles
	nohup bash hack/scripts/switch-wals-25.sh &
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/allocs
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/heap
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/profile?seconds=10
	curl -s http://127.0.0.1:7070/debug/pprof/trace\?seconds\=10 | go tool trace /dev/stdin

.PHONY: clean
clean: ## Remove build artifacts and logs
	@rm -rf bin/ dist/ test/integration/environ/bin/ *.log

.PHONY: help
help: ## Show this help
	@echo "Usage: make <target>"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'
