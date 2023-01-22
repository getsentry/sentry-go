### Inspired by https://github.com/open-telemetry/opentelemetry-go/blob/main/Makefile

.DEFAULT_GOAL := help

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
MKFILE_DIR := $(dir $(MKFILE_PATH))
ALL_GO_MOD_DIRS := $(shell find . -type f -name 'go.mod' -exec dirname {} \; | sort)
GO = go
TIMEOUT = 60

# Tools

TOOLS = $(CURDIR)/.tools

$(TOOLS):
	@mkdir -p $@
$(TOOLS)/%: | $(TOOLS)
	cd $(TOOLS_MOD_DIR) && \
	$(GO) build -o $@ $(PACKAGE)

GOCOVMERGE = $(TOOLS)/gocovmerge
$(TOOLS)/gocovmerge: PACKAGE=github.com/wadey/gocovmerge


# Parse Makefile and display the help
help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
.PHONY: help

build: ## Build everything
	go build ./...
.PHONY: build


# Tests
TEST_TARGETS := test-short test-verbose test-race
.PHONY: $(TEST_TARGETS) test
test-race: ARGS=-race
test-short:   ARGS=-short
test-verbose: ARGS=-v -race
$(TEST_TARGETS): test
test: $(ALL_GO_MOD_DIRS:%=test/%)
test/%: DIR=$*
test/%:
	@echo ">>> Running tests for module: $(DIR)"
	@# We use '-count=1' to disable test caching.
	cd $(DIR) && $(GO) test -count=1 -timeout $(TIMEOUT)s $(ARGS) ./...

# Coverage
COVERAGE_MODE    = atomic
COVERAGE_PROFILE = coverage.out
COVERAGE_REPORT_DIR = .coverage
COVERAGE_REPORT_DIR_ABS = "$(MKFILE_DIR)/$(COVERAGE_REPORT_DIR)"
.PHONY: test-coverage clean-report-dir
$(COVERAGE_REPORT_DIR):
	mkdir -p $(COVERAGE_REPORT_DIR)
clean-report-dir: $(COVERAGE_REPORT_DIR)
	test $(COVERAGE_REPORT_DIR) && rm -f $(COVERAGE_REPORT_DIR)/*
test-coverage: $(COVERAGE_REPORT_DIR) clean-report-dir
	@set -e; \
	for dir in $(ALL_GO_MOD_DIRS); do \
	  echo "$(GO) test -count=1 -covermode=$(COVERAGE_MODE) -coverprofile="$(COVERAGE_PROFILE)" $${dir}/..."; \
	  (cd "$${dir}" && \
	    $(GO) test -count=1 -coverpkg=./... -covermode=$(COVERAGE_MODE) -coverprofile="$(COVERAGE_PROFILE)" && \
		cp $(COVERAGE_PROFILE) "$(COVERAGE_REPORT_DIR_ABS)/$${RANDOM}_$(COVERAGE_PROFILE)" && \
	    $(GO) tool cover -html=$(COVERAGE_PROFILE) -o coverage.html); \
	done;

vet: ## Run "go vet"
	go vet ./...
.PHONY: vet

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
