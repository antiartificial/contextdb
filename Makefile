MODULE := github.com/antiartificial/contextdb
BIN    := contextdb
GOFLAGS := -mod=mod

.PHONY: all build test test-verbose cover bench longmemeval fitness run lint tidy clean helm-lint help

all: tidy build test ## Build, then run tests (default)

build: ## Build the binary to ./bin/contextdb
	@mkdir -p bin
	go build $(GOFLAGS) -o bin/$(BIN) ./cmd/$(BIN)

run: build ## Build and run the embedded demo
	./bin/$(BIN)

test: ## Run all tests (short output)
	go test $(GOFLAGS) ./...

test-verbose: ## Run all tests with verbose output
	go test $(GOFLAGS) -v ./...

test-core: ## Run core package tests only
	go test $(GOFLAGS) -v ./internal/core/...

test-ingest: ## Run ingest (admission) tests only
	go test $(GOFLAGS) -v ./internal/ingest/...

test-retrieval: ## Run retrieval fusion tests only
	go test $(GOFLAGS) -v ./internal/retrieval/...

bench: ## Run bench visualisation tests (writes /tmp/contextdb_bench.html)
	go test $(GOFLAGS) -v -run TestBench ./bench/...
	@echo ""
	@echo "HTML report: /tmp/contextdb_bench.html"

fitness: ## Run fitness evaluation suite
	go test $(GOFLAGS) -v -run TestFitness ./bench/...

longmemeval: ## Run LongMemEval benchmark (synthetic dataset)
	go test $(GOFLAGS) -v -run TestRunner ./bench/longmemeval/...

helm-lint: ## Lint Helm chart
	helm lint deploy/helm/contextdb

cover: ## Run tests with coverage and open HTML report
	@mkdir -p .coverage
	go test $(GOFLAGS) -coverprofile=.coverage/coverage.out ./...
	go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html
	@echo "Coverage report: .coverage/coverage.html"

cover-text: ## Show coverage summary in terminal
	@mkdir -p .coverage
	go test $(GOFLAGS) -coverprofile=.coverage/coverage.out ./...
	go tool cover -func=.coverage/coverage.out

tidy: ## Tidy go.mod and go.sum
	GONOSUMDB='*' GOPROXY=direct go mod tidy 2>/dev/null || go mod tidy

lint: ## Run go vet (add golangci-lint if available)
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin/ .coverage/

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
