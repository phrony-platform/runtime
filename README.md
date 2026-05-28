<p align="center" style="padding-top: 2rem; padding-bottom: 2rem;">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-light.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-dark.png">
    <img alt="Phrony" src="assets/logo-dark.png" width="400">
  </picture>
</p>

# Phrony Runtime

**Official open-source implementation of the Phrony Agent Spec runtime.**

This repository implements the runtime defined by the Phrony Agent Spec—the open specification for declaring, deploying, and running agents as first-class primitives. In the Phrony paradigm, agents are **declared** in a manifest, **deployed** to a runtime, and **run** as named, versioned entities—not embedded as application code. This runtime loads deployed manifests, executes agent sessions (model loop, tool mediation, policies, limits, and human-in-the-loop), emits structured traces, and returns results. Applications ask the runtime for work; the runtime is where the agent lives.

## Prerequisites

- Go 1.25+
- Docker (local Postgres via `docker compose`)

## Getting started

### 1. Postgres and environment

Copy the example env file and adjust values if needed (connection URL, gRPC addresses):

```bash
cp .env.example .env
```

Start Postgres and apply schema migrations:

```bash
make dev-up
```

`make dev-up` runs `docker compose up` for Postgres, then `phrony-runtime migrate`. To stop Postgres later: `make dev-down`.

Load the env in your shell before running binaries (or use the `make` shortcuts in [Development](#development)):

```bash
set -a && source .env && set +a
```

| Variable | Description |
|----------|-------------|
| `RUNTIME_DATABASE_URL` | Postgres connection string for `phrony-runtime` |
| `RUNTIME_GRPC_ADDR` | gRPC listen address (default `127.0.0.1:7777`) |
| `PHRONY_RUNTIME_ADDR` | Runtime endpoint for the `phrony` CLI (optional override) |

### 2. Install binaries

Install both binaries into `$(go env GOPATH)/bin` (must be on your `PATH`):

```bash
go install github.com/phrony-platform/runtime/cmd/phrony-runtime@latest
go build -o "$(go env GOPATH)/bin/phrony" github.com/phrony-platform/runtime/cmd/cli@latest
```

The runtime installs as `phrony-runtime` via `go install`. The operator CLI package lives at `cmd/cli`, so use `go build -o` to install it as `phrony` rather than `cli`.

**Build from source:**

```bash
git clone https://github.com/phrony-platform/runtime.git
cd runtime
make build
export PATH="$(pwd)/bin:$PATH"
```

### 3. Run the runtime

With `.env` loaded (step 1):

```bash
phrony-runtime serve
```

`serve` runs migrations (unless `--skip-migrate`) and starts the gRPC server. Migrations only:

```bash
phrony-runtime migrate
```

### 4. Operator CLI

In another terminal, load `.env` again, then:

```bash
phrony status
phrony run <session-id>
phrony deploy --file path/to/manifest.yaml
```

Pass `--runtime-addr` to override `PHRONY_RUNTIME_ADDR`.

## Components

| Binary | Role |
|--------|------|
| `phrony-runtime` | Daemon: migrations, gRPC server, manifest deploy, session execution |
| `phrony` | Operator CLI over gRPC (`status`, `run`, `deploy`) |

The Node-based Phrony CLI (manifest authoring and packaging) is separate; this repo is the official Phrony Agent Spec runtime daemon and its Go operator tools.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for repository layout, code style, and pull request guidelines.

From the repo root, `make` loads `.env` (or `.env.example`) automatically:

| Target | Description |
|--------|-------------|
| `make dev-up` | Start Postgres and run migrations |
| `make dev-down` | Stop Postgres |
| `make migrate` | Migrations only (Postgres already running) |
| `make serve` | Run `phrony-runtime serve` |
| `make cli …` | Operator CLI (e.g. `make cli status`) |
| `make build` | Build `bin/phrony` and `bin/phrony-runtime` |
| `make test` | Unit tests (`-short`) |
| `make test-coverage` | Coverage report for `internal/` |
| `make proto` | Regenerate `gen/` from `proto/` |

Run `make` for the full target list. Install protobuf plugins once: `make proto-tools` (requires `protoc`).

```bash
make test
go test ./...
```
