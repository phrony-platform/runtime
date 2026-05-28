# Contributing to Phrony Runtime

Thank you for your interest in contributing. This document covers how to set up a development environment, how the repository is organized, and what we expect in pull requests.

For running the runtime locally, see [README.md](README.md).

## Development setup

1. Install **Go 1.25+** and **Docker**.
2. Clone the repository and copy environment config:

   ```bash
   git clone https://github.com/phrony-platform/runtime.git
   cd runtime
   cp .env.example .env
   ```

3. Start Postgres and apply migrations:

   ```bash
   make dev-up
   ```

4. Build binaries:

   ```bash
   make build
   ```

5. Run tests before opening a PR:

   ```bash
   make test
   ```

`make` loads `.env` when present, otherwise `.env.example`. Run `make` without arguments for all targets.

## Repository structure

```
runtime/
├── assets/                 Brand assets (logos)
├── cmd/
│   ├── cli/                Operator CLI (`phrony`) — thin gRPC client
│   └── phrony-runtime/     Daemon entrypoint (`phrony-runtime`)
├── gen/                    Generated Go from protobuf (do not edit by hand)
├── internal/
│   ├── clierr/             CLI error formatting
│   ├── cliout/             CLI output (status panel, branding)
│   ├── common/             Shared config, DB helpers, validation
│   ├── core/               gRPC server, migrations, runtime logic
│   ├── model/              Domain types and persistence models
│   └── runtimecmd/         Cobra commands for `phrony-runtime`
├── proto/                  Protobuf source (edit here, then regenerate)
│   ├── grpc/health/v1/     Standard gRPC health definitions
│   └── phrony/runtime/v1/  Phrony runtime API
├── bin/                    Built binaries (`make build`; gitignored)
├── docker-compose.yml      Local Postgres for development
├── .env.example            Example environment variables
├── Makefile                Dev workflows (test, serve, proto, …)
└── go.mod
```

| Path | Purpose |
|------|---------|
| `cmd/` | Entrypoints only — keep logic in `internal/` |
| `internal/` | Private application code; not importable by other modules |
| `gen/` | Output of `make proto` — commit regenerated files with API changes |
| `proto/` | API contract; changes should align with the Phrony Agent Spec |

## Making changes

### Scope

- Keep pull requests focused. Prefer several small PRs over one large change.
- Match existing patterns in the package you are editing (naming, error handling, command structure).
- Do not commit secrets (`.env`, credentials). `.env` is gitignored.

### Protobuf and generated code

If you change files under `proto/`:

```bash
make proto-tools   # once: installs protoc Go plugins
make proto         # regenerates gen/
```

Commit both `proto/` and `gen/` changes together. Requires `protoc` on your `PATH`.

### Database migrations

Schema changes belong in `internal/core` migration logic. Test with a fresh database:

```bash
make dev-down && make dev-up
make migrate
```

## Code style

This project follows standard Go conventions.

| Topic | Expectation |
|-------|-------------|
| Formatting | `gofmt` / `go fmt ./...` — CI and reviewers expect default Go formatting |
| Imports | `goimports` grouping (stdlib, third-party, module) |
| Comments | `//` for comments; document exported symbols and non-obvious behavior |
| Errors | Wrap with context (`fmt.Errorf("…: %w", err)`); use `clierr` for CLI-facing gRPC errors |
| Tests | Table-driven tests where appropriate; place `*_test.go` next to the code under test |
| Commands | Cobra in `cmd/` and `internal/runtimecmd/`; flags and env via `internal/common` |

Run formatting before pushing:

```bash
go fmt ./...
```

## Tests

```bash
make test              # short mode, all packages
make test-coverage     # coverage for internal/
go test ./...          # full test run
```

Add or update tests for behavior you change. Integration tests that need Postgres should use `make dev-up` and the test database URL from `.env`.

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your changes with tests passing locally.
3. Open a pull request with:
   - A clear summary of **what** changed and **why**
   - Notes on testing performed (commands run, manual steps if any)
   - Mention of any API or migration impact
4. Address review feedback with additional commits or squashed updates as requested.

We may ask you to rebase on `main` if the branch falls behind.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/): a type prefix, optional scope in parentheses, then a short imperative description.

```
feat(cli): show gRPC health in status panel
fix(core): handle missing schema version row
chore: bump go.mod dependencies
docs: add contributing guide
test(runtimecmd): cover serve with skip-migrate
```

Common types:

| Type | Use for |
|------|---------|
| `feat` | New behavior or capability |
| `fix` | Bug fixes |
| `docs` | Documentation only |
| `test` | Tests only |
| `chore` | Tooling, deps, housekeeping |
| `refactor` | Code change without behavior change |

Keep the subject line ≤72 characters. Add a body after a blank line when the change needs context. One logical change per commit.

## Questions

Open a GitHub issue for bugs, feature proposals, or questions about contributing. For spec-level behavior, refer to the Phrony Agent Spec when discussing runtime semantics.
