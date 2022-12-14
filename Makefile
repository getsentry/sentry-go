.DEFAULT_GOAL := help

# Parse Makefile and display the help
help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
.PHONY: help

build: ## Build everything
	go build ./...
.PHONY: build

vet: ## Run "go vet"
	go vet ./...
.PHONY: vet

test: ## Run tests
	go test -count=1 ./...
.PHONY: test

test-with-race-detection: ## Run tests with data race detector
	go test -count=1 -race ./...
.PHONY: test-with-race-detection

test-with-coverage: ## Test with coverage enabled
	go test -count=1 -race -coverprofile=coverage.txt -covermode=atomic ./...
.PHONY: test-with-coverage

coverage-report: test-with-coverage ## Test with coverage and open the produced HTML report
	go tool cover -html coverage.txt
.PHONY: coverage-report

lint: ## Lint (using "golangci-lint")
	golangci-lint run
.PHONY: lint

fmt: ## Format all Go files
	gofmt -l -w -s .
.PHONY: fmt
