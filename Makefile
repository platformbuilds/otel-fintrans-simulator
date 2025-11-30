.PHONY: all build test run localdev-sim tidy fmt

all: build

build:
	# Build the whole package (not a single file) so all sources in package main are compiled
	go build -o bin/otel-fintrans-simulator .

test:
	go test ./...

run:
	go run main.go

localdev-sim: build
	# Quick local dev run against no-op logging / default OTLP endpoint
	./bin/otel-fintrans-simulator --transactions 10 --log-output stdout

tidy:
	go mod tidy

fmt:
	gofmt -s -w .
