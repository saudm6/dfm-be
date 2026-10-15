# Override from the environment, e.g. DFM_HTTP_ADDR=:18080 make run.
# The database default matches docker-compose.yml (docker compose up -d db).
LOCAL_DATABASE_URL := postgres://dfm:dfm@localhost:5432/dfm?sslmode=disable

BIN := bin/dfm$(shell go env GOEXE)
SQLC_IMAGE := sqlc/sqlc:1.31.1

.PHONY: build test lint run generate migrate sqlc

build:
	go build -o $(BIN) ./cmd/dfm

test:
	go test -race ./...

lint:
	golangci-lint run

run: export DFM_HTTP_ADDR ?= :8080
# make run serves plain http, so the session cookie must not be Secure.
run: export DFM_COOKIE_SECURE ?= false
run migrate: export DFM_DATABASE_URL ?= $(LOCAL_DATABASE_URL)

run:
	go run ./cmd/dfm

migrate:
	go run ./cmd/dfm migrate

# Regenerates internal/store/dbgen from sqlc.yaml, queries/ and migrations/.
sqlc:
	docker run --rm -v "$(CURDIR):/src" -w /src $(SQLC_IMAGE) generate

# Regenerates internal/httpapi/gen from api/openapi.yaml.
generate:
	go generate ./...
