.PHONY: run build tidy test test-e2e test-e2e-validate dev-up dev-down fresh-db

RUNTIME_ROOT := ..
RUNTIME_COMPOSE := -f $(RUNTIME_ROOT)/docker-compose.yml -f docker-compose.e2e.yml

dev-up:
	docker compose $(RUNTIME_COMPOSE) up -d --build --wait

dev-down:
	docker compose $(RUNTIME_COMPOSE) down

fresh-db:
	bash scripts/fresh-db.sh

run:
	go run ./cmd/worker

build:
	go build -o bin/worker ./cmd/worker

tidy:
	go mod tidy

test:
	go test ./internal/...

test-e2e-validate:
	@rm -f e2e/scenario-overview.txt
	E2E_LOG=1 go test -v -shuffle=off ./e2e -count=1
	@test -f e2e/scenario-overview.txt && cat e2e/scenario-overview.txt

# E2E_LOG=1 + -v: go test hides passing-test output without -v; overview still printed at end.
# -shuffle=off keeps zzz_archive then zzz_overview last (archive must not run before B3/D1).
test-e2e: fresh-db
	@rm -f e2e/scenario-overview.txt
	E2E_LOG=1 go test -v -shuffle=off -tags=integration ./e2e -count=1 -timeout 15m
	@test -f e2e/scenario-overview.txt && cat e2e/scenario-overview.txt
