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

### 1. Run with Docker Compose

From a clone of this repository, start Postgres and the runtime with one command:

```bash
cp .env.example .env   # optional; used by the operator CLI on your host
docker compose up --build
```

Or in the background:

```bash
make dev-up
```

Compose starts Postgres, waits until it is healthy, builds `phrony-runtime`, runs migrations on startup, and listens on **gRPC port 7777** (`127.0.0.1:7777` from your machine). Stop the stack with `docker compose down` or `make dev-down`.

### 2. Environment (operator CLI on the host)

Copy `.env.example` to `.env` so the `phrony` CLI can reach the containerized runtime. Load it when running CLI commands outside `make`:

```bash
set -a && source .env && set +a
```

| Variable | Description |
|----------|-------------|
| `RUNTIME_DATABASE_URL` | Postgres URL when running `phrony-runtime` on the host |
| `RUNTIME_GRPC_ADDR` | gRPC listen address on the host (default `127.0.0.1:7777`) |
| `PHRONY_RUNTIME_ADDR` | Runtime endpoint for the `phrony` CLI (default `127.0.0.1:7777`) |

### 3. Install binaries

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

### 4. Run the runtime on the host (optional)

For Go development, start only Postgres in Compose, then run the daemon locally:

```bash
docker compose up -d postgres --wait
make migrate    # or: phrony-runtime migrate
make serve      # or: phrony-runtime serve
```

`serve` runs migrations (unless `--skip-migrate`) and starts the gRPC server. Do not run `make serve` while the compose `runtime` service is also bound to port 7777.

### 5. Operator CLI

In another terminal, load `.env` again, then:

```bash
phrony status
phrony validate path/to/agent.yaml
phrony deploy path/to/agent.yaml
phrony run <namespace>/<name>
phrony run <namespace>/<name> -v 1.2.0
```

Pass `--runtime-addr` to override `PHRONY_RUNTIME_ADDR`.

## Components

| Binary | Role |
|--------|------|
| `phrony-runtime` | Daemon: migrations, gRPC server, manifest deploy, session execution |
| `phrony` | Operator CLI over gRPC (`status`, `run`, `deploy`) and local manifest checks (`validate`) |

The Node-based Phrony CLI (manifest authoring and packaging) is separate; this repo is the official Phrony Agent Spec runtime daemon and its Go operator tools.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for repository layout, code style, and pull request guidelines.

From the repo root, `make` loads `.env` (or `.env.example`) automatically:

| Target | Description |
|--------|-------------|
| `make dev-up` | Start Postgres + runtime (`docker compose up`) |
| `make dev-down` | Stop the compose stack |
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
