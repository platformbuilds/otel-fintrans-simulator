.PHONY: help all build test run localdev-sim tidy fmt

## Usage:
#  - `make help`                 : show this help
#  - `make build`                : build the simulator binary (bin/otel-fintrans-simulator)
#  - `make test`                 : run unit tests
#  - `make run`                  : run using `go run main.go`
#  - `make localdev-sim`         : build and run a short demo targeting stdout
#  - `make tidy`                 : run `go mod tidy`
#  - `make fmt`                  : run `gofmt -s -w .`
#
# Post-build quick commands (examples you can run after `make build`):
#  - Run the built simulator locally:
#      ./bin/otel-fintrans-simulator --config simulator-config.yaml --log-output stdout
#  - Run with a deterministic RNG seed for reproducible runs:
#      ./bin/otel-fintrans-simulator --rand-seed 12345 --log-output stdout
#  - Run a short demo with fixed TPS:
#      TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --log-output stdout

all: build

build: ## Build the whole package (output: bin/otel-fintrans-simulator)
	# Build the whole package (not a single file) so all sources in package main are compiled
	go build -o bin/otel-fintrans-simulator .

test: ## Run unit tests
	go test ./...

run: ## Run using `go run main.go`
	go run main.go

localdev-sim: build ## Quick local dev run against no-op logging / default OTLP endpoint
	# Quick local dev run against no-op logging / default OTLP endpoint
	./bin/otel-fintrans-simulator --transactions 10 --log-output stdout

tidy: ## Run `go mod tidy`
	go mod tidy

fmt: ## Format code using gofmt
	gofmt -s -w .

help: ## Show this help message
	@echo "Usage: make <target>"
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*##"; printf "\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo "\nExamples (post-build):"
	@echo "  ./bin/otel-fintrans-simulator --config simulator-config.yaml --log-output stdout"
	@echo "  ./bin/otel-fintrans-simulator --rand-seed 12345 --log-output stdout"
	@echo "  TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --log-output stdout"

