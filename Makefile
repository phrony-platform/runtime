.PHONY: proto proto-tools proto-tools-check build build-phrony build-runtime test test-coverage test-coverage-html

GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(GOPATH_BIN):$(PATH)

PROTO_DIR := proto
GEN_DIR := gen

COVERAGE_OUT := coverage.out
COVERAGE_HTML := coverage.html
# Application packages only (excludes gen/ and cmd/ thin mains).
COVER_PKGS := ./internal/...

# Install protoc plugins (protoc itself must be installed separately, e.g. brew install protobuf).
proto-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

proto: proto-tools-check
	@mkdir -p $(GEN_DIR)
	protoc -I $(PROTO_DIR) \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/phrony/runtime/v1/*.proto $(PROTO_DIR)/grpc/health/v1/*.proto

proto-tools-check:
	@command -v protoc >/dev/null 2>&1 || { \
		echo "protoc not found; install it (e.g. brew install protobuf)"; exit 1; \
	}
	@command -v protoc-gen-go >/dev/null 2>&1 || { \
		echo "protoc-gen-go not found; run: make proto-tools"; exit 1; \
	}
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { \
		echo "protoc-gen-go-grpc not found; run: make proto-tools"; exit 1; \
	}

build: build-phrony build-runtime

build-phrony:
	go build -o bin/phrony ./cmd/cli

build-runtime:
	go build -o bin/phrony-runtime ./cmd/phrony-runtime

test:
	go test -short ./...

test-coverage:
	go test -coverprofile=$(COVERAGE_OUT) $(COVER_PKGS)
	go tool cover -func=$(COVERAGE_OUT)

test-coverage-html: test-coverage
	go tool cover -html=$(COVERAGE_OUT) -o $(COVERAGE_HTML)
	@echo "Wrote $(COVERAGE_HTML)"
