SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# PostgreSQL DSN used by the migrate-* targets.
# Override with: make migrate-up DSN=postgres://user:pass@host:5432/db?sslmode=disable
DSN ?= postgres://gophkeeper:gophkeeper@localhost:5432/gophkeeper?sslmode=disable

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)

.PHONY: all lint test build build-server build-client docker-build generate migrate-up migrate-down

all: lint test build

## lint: run golangci-lint
lint:
	golangci-lint run

## test: run all tests with race detector and coverage
test:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out | awk '/^total:/ {print "Total coverage:", $$3}'

## build: build server and client binaries into bin/
build: build-server build-client

## build-server: build the server binary into bin/
build-server:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/gophkeeper-server ./cmd/server

## build-client: build the client binary into bin/
build-client:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/gophkeeper-client ./cmd/client

## docker-build: build the server Docker image
docker-build:
	docker build -f Dockerfile.server \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t gophkeeper-server .

## generate: regenerate Go code from proto/ into internal/common/proto/gophkeeperv1/
## Requires buf (https://buf.build) and plugins:
##   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
##   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
## protoc fallback (if buf is unavailable):
##   protoc -I proto \
##     --go_out=. --go_opt=module=github.com/dmitriy/gophkeeper \
##     --go-grpc_out=. --go-grpc_opt=module=github.com/dmitriy/gophkeeper \
##     proto/gophkeeper/v1/*.proto
generate:
	buf generate proto

## migrate-up: apply all pending migrations (requires migrate CLI)
##   Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
migrate-up:
	migrate -path migrations -database "$(DSN)" up

## migrate-down: roll back the last migration (requires migrate CLI)
migrate-down:
	migrate -path migrations -database "$(DSN)" down 1
