.PHONY: targets dev-up dev-down migrate serve build install-cli test test-coverage proto proto-tools cli

GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(GOPATH_BIN):$(PATH)

PROTO_DIR := proto
GEN_DIR := gen
BIN_DIR := bin

COVERAGE_OUT := coverage.out
COVER_PKGS := ./internal/...

# Passthrough for "make cli ..." only (e.g. make cli status). Do not treat other targets
# like install-cli as CLI args — that would shadow real Makefile targets.
ifeq ($(firstword $(MAKECMDGOALS)),cli)
CLI_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
ifneq ($(CLI_ARGS),)
$(CLI_ARGS):
	@:
endif
endif

# Load .env if present, otherwise .env.example.
LOAD_ENV = set -a && { [ -f .env ] && . ./.env || . ./.env.example; } && set +a

.DEFAULT_GOAL := targets

targets:
	@echo "Local dev (copy .env.example to .env once):"
	@echo "  make dev-up              Postgres + runtime (docker compose up)"
	@echo "  make dev-down            Stop compose stack"
	@echo "  make migrate             Migrations only (Postgres already up)"
	@echo "  make migrate-create name=description   New SQL migration pair in migrations/"
	@echo "  make serve               Run phrony-runtime (foreground)"
	@echo "  make cli ...             Operator CLI via go run (flags like --namespace break make)"
	@echo "  make install-cli         Install phrony to GOPATH/bin and ~/.local/bin; update shell PATH"
	@echo ""
	@echo "Build & test:"
	@echo "  make build               bin/phrony, bin/phrony-runtime"
	@echo "  make test"
	@echo "  make test-coverage       internal/ coverage (HTML: go tool cover -html=$(COVERAGE_OUT))"
	@echo ""
	@echo "  make proto               Regenerate gen/ (run make proto-tools once if needed)"

dev-up:
	docker compose up -d --build --wait

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

# Install to $(go env GOPATH)/bin (or GOBIN) and ~/.local/bin; append PATH in shell rc once.
# Prefer this over "make cli" so flags like --namespace are not parsed by make.
install-cli:
	@bin=$$(go env GOBIN); \
	if [ -z "$$bin" ]; then bin="$$(go env GOPATH)/bin"; fi; \
	install_dir="$${HOME}/.local/bin"; \
	mkdir -p "$$bin" "$$install_dir"; \
	go build -o "$$bin/phrony" ./cmd/cli; \
	cp "$$bin/phrony" "$$install_dir/phrony"; \
	PHRONY_INSTALL_DIR="$$install_dir" sh scripts/setup-cli-path.sh; \
	echo "Installed phrony to $$bin/phrony and $$install_dir/phrony"

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
