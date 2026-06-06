<p style="display: block; width: 100%; padding-top: 2rem; padding-bottom: 2rem;">
  <picture>
    <source srcset="assets/phrony-runtime-logo.png">
    <img alt="Phrony" src="assets/logo-dark.png" style="width: 100%; max-width: 600px; display: block; margin: 0 auto;">
  </picture>
</p>

# Phrony Runtime

**Official open-source implementation of the Phrony Agent Spec runtime.**

This repository implements the runtime defined by the Phrony Agent Spec—the open specification for declaring, deploying, and running agents as first-class primitives. In the Phrony paradigm, agents are **declared** in a manifest, **deployed** to a runtime, and **run** as named, versioned entities—not embedded as application code. This runtime loads deployed manifests, executes agent sessions (model loop, tool mediation, policies, limits, and human-in-the-loop), emits structured traces, and returns results. Applications ask the runtime for work; the runtime is where the agent lives.

## Prerequisites

- Docker (Postgres + runtime via Docker Compose)
- Go 1.25+ (operator CLI)

## Getting started

Three steps take you from zero to a running runtime you can drive with the operator CLI.

### 1. Bring up the runtime

Use the official Docker image `ghcr.io/phrony-platform/phrony-runtime:latest` (published on each [GitHub release](https://github.com/phrony-platform/runtime/releases)).

Download the Compose file from the [Phrony docs](https://phrony.com/runtime/docker-compose.yml), then start Postgres and the runtime:

```bash
mkdir phrony-runtime && cd phrony-runtime
curl -fsSLO https://phrony.com/runtime/docker-compose.yml
docker compose up -d --wait
```

Compose waits for Postgres to become healthy, pulls `phrony-runtime`, runs migrations on startup, and listens on **gRPC port 7777** (`127.0.0.1:7777` from your machine). Stop the stack with `docker compose down`.

### 2. Install the operator CLI

Install `phrony` with Go and ensure `$(go env GOPATH)/bin` is on your `PATH`:

```bash
go build -o "$(go env GOPATH)/bin/phrony" \
  github.com/phrony-platform/runtime/cmd/cli@latest
```

From a clone of this repository you can instead run `make install-cli`, which also installs to `~/.local/bin` and updates your shell `PATH`.

### 3. Use the runtime

With the runtime up and `phrony` on your `PATH`:

```bash
phrony status
phrony validate path/to/agent.yaml
phrony deploy path/to/agent.yaml
phrony session <namespace>/<name>
phrony session <namespace>/<name> -v 1.2.0
```

Pass `--runtime-addr` to override the default runtime endpoint (`127.0.0.1:7777`).

## Environment variables

The [Compose file](https://phrony.com/runtime/docker-compose.yml) sets `RUNTIME_DATABASE_URL` (hostname `postgres`), `RUNTIME_GRPC_ADDR=0.0.0.0:7777`, and a dev `RUNTIME_SECRETS_ENCRYPTION_KEY` on the runtime service, so the quickstart above needs no extra configuration.

When building from source, `make` targets load `.env` (or `.env.example` as a fallback) automatically. Copy `.env.example` to `.env` to customize host-side settings.

| Variable | Used by | Description |
|----------|---------|-------------|
| `RUNTIME_DATABASE_URL` | `phrony-runtime` on the host | Postgres connection string (`localhost:5432` with compose Postgres) |
| `RUNTIME_GRPC_ADDR` | `phrony-runtime` on the host | gRPC listen address (default `127.0.0.1:7777`; use `0.0.0.0:7777` inside Docker) |
| `RUNTIME_SECRETS_ENCRYPTION_KEY` | `phrony-runtime` | AES-256 master key for encrypting publish-time secrets (32 bytes, base64 or hex). Required when deploying agents with a `secrets` section. Generate with `openssl rand -base64 32` |
| `RUNTIME_TOOL_ALLOWLIST` | `phrony-runtime` | Path to a YAML tool allowlist for dispatch-time integrity checks (optional) |
| `RUNTIME_DISPATCH_QUEUE_WAIT` | `phrony-runtime` | Max time a tool call may wait in the worker queue when no handler is free (default `10s`). Go duration (`5s`, `500ms`) or positive integer seconds (`30`). Invalid values fall back to `10s`. Applied even when the session wall-clock budget is longer, so detached runs do not park in `awaiting_tool` until the session limit |
| `RUNTIME_ENABLE_STUB_PROVIDER` | `phrony-runtime` | Dev-only: enable the scripted stub model provider (`true`, `1`, or `yes`) |
| `PHRONY_RUNTIME_ADDR` | `phrony` on the host | Runtime gRPC endpoint (default `127.0.0.1:7777` when using compose) |
| `PHRONY_ACTOR` | `phrony` on the host | Audit identity for publish, deploy, and rollback (defaults to OS username) |
| `PHRONY_NO_TUI` | `phrony` | Disable the interactive session TUI (plain stdout) |
| `NO_COLOR` | `phrony diff` | Disable colorized diff output (also disabled when stdout is not a TTY) |

Provider API keys referenced in manifest `secrets.*.fromEnv` (for example `ANTHROPIC_API_KEY`) are read on the machine running `phrony publish`, not on the runtime daemon.

## Components

| Binary | Role |
|--------|------|
| `phrony-runtime` | Daemon: migrations, gRPC server, manifest deploy, session execution |
| `phrony` | Operator CLI over gRPC (`status`, `session`, `deploy`) and local manifest checks (`validate`) |

The Node-based Phrony CLI (manifest authoring and packaging) is separate; this repo is the official Phrony Agent Spec runtime daemon and its Go operator tools.

## Development

To build the runtime from source instead of pulling the official image, clone this repository and use `make dev-up` (builds the local `Dockerfile`) or `make serve` with Postgres from Compose. For building binaries, tests, migrations, protobuf regeneration, and other workflows, run `make` with no arguments to see the full target list.

See [CONTRIBUTING.md](CONTRIBUTING.md) for repository layout, code style, the development setup, and pull request guidelines.
