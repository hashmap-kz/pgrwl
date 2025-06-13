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

# Lint the code
.PHONY: lint
lint:
	golangci-lint run --output.tab.path=stdout

.PHONY: gen
gen:
	go generate ./...

# Build the binary
.PHONY: build
build: gen
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
	docker buildx build -t localhost:5000/pgrwl .
	docker push localhost:5000/pgrwl

## Profiling

.PHONY: run
run: build
	export PGHOST="localhost" && \
	export PGPORT="5432" && \
	export PGUSER="postgres" && \
	export PGPASSWORD="postgres" && \
	bin/$(OUTPUT) start -c hack/configs/localfs/receive.yml -m receive

.PHONY: profile-cpu
profile-cpu:
	nohup bash hack/scripts/switch-wals-25.sh &
	go tool pprof -http=: http://localhost:7070/debug/pprof/profile?seconds=20

.PHONY: pprof1
pprof1:
	nohup bash hack/scripts/switch-wals-25.sh &
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/allocs
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/heap
	go tool pprof -web http://127.0.0.1:7070/debug/pprof/profile?seconds=10
	curl -s http://127.0.0.1:7070/debug/pprof/trace\?seconds\=10 | go tool trace /dev/stdin

# Cleanup

.PHONY: clean
clean:
	@rm -rf bin/ dist/ test/integration/environ/bin/ *.log
