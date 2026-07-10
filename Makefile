.PHONY: build test test-unit lint fmt

build:
	mkdir -p bin
	go build -o bin/lispico ./cmd/lispico

test:
	go test ./...

test-unit: test

lint:
	golangci-lint run

fmt:
	go fmt ./...
