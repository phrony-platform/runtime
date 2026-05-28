.PHONY: targets dev-up dev-down migrate serve build test test-coverage proto proto-tools cli

GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(GOPATH_BIN):$(PATH)

PROTO_DIR := proto
GEN_DIR := gen
BIN_DIR := bin

COVERAGE_OUT := coverage.out
COVER_PKGS := ./internal/...

# Words after "cli" on the make command line, e.g. make cli status deploy agent.yaml
CLI_ARGS := $(filter-out cli,$(MAKECMDGOALS))
ifneq ($(CLI_ARGS),)
$(CLI_ARGS):
	@:
endif

# Load .env if present, otherwise .env.example.
LOAD_ENV = set -a && { [ -f .env ] && . ./.env || . ./.env.example; } && set +a

.DEFAULT_GOAL := targets

targets:
	@echo "Local dev (copy .env.example to .env once):"
	@echo "  make dev-up              Postgres + migrate"
	@echo "  make dev-down            Stop Postgres"
	@echo "  make migrate             Migrations only (Postgres already up)"
	@echo "  make migrate-create name=description   New SQL migration pair in migrations/"
	@echo "  make serve               Run phrony-runtime (foreground)"
	@echo "  make cli ...             Operator CLI (runtime must be running)"
	@echo ""
	@echo "Build & test:"
	@echo "  make build               bin/phrony, bin/phrony-runtime"
	@echo "  make test"
	@echo "  make test-coverage       internal/ coverage (HTML: go tool cover -html=$(COVERAGE_OUT))"
	@echo ""
	@echo "  make proto               Regenerate gen/ (run make proto-tools once if needed)"

dev-up:
	docker compose up -d --wait
	@$(LOAD_ENV) && go run ./cmd/phrony-runtime migrate

dev-down:
	docker compose down

migrate:
	@$(LOAD_ENV) && go run ./cmd/phrony-runtime migrate

migrate-create:
	@test -n "$(name)" || { echo "usage: make migrate-create name=short_description"; exit 1; }
	migrate create -ext sql -dir migrations -seq $(name)

serve:
	@$(LOAD_ENV) && go run ./cmd/phrony-runtime serve

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/phrony ./cmd/cli
	go build -o $(BIN_DIR)/phrony-runtime ./cmd/phrony-runtime

cli:
	@$(LOAD_ENV) && go run ./cmd/cli $(CLI_ARGS)

test:
	go test -short ./...

test-coverage:
	go test -coverprofile=$(COVERAGE_OUT) $(COVER_PKGS)
	go tool cover -func=$(COVERAGE_OUT)

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
