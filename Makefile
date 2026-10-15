# Override the listen address from the environment, e.g. DFM_HTTP_ADDR=:18080 make run.
DFM_HTTP_ADDR ?= :8080
export DFM_HTTP_ADDR

BIN := bin/dfm$(shell go env GOEXE)

.PHONY: build test lint run generate

build:
	go build -o $(BIN) ./cmd/dfm

test:
	go test -race ./...

lint:
	golangci-lint run

run:
	go run ./cmd/dfm

# Regenerates internal/httpapi/gen from api/openapi.yaml.
generate:
	go generate ./...
