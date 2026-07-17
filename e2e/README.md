# Runtime e2e suite

Nested Go module (`github.com/phrony-platform/runtime/e2e`) for scenario integration tests against a running Phrony runtime. It is **not** part of the `phrony` / `phrony-runtime` binaries: the root module never imports these packages, Docker builds exclude this tree (`.dockerignore`), and `make build` at the repo root only builds `./cmd/cli` and `./cmd/phrony-runtime`.

Local application worker connects to `Runtime.Work`, advertises handlers for payment tools declared in scenario bundles, and executes invocations in-process.

Scenarios under [`scenarios/`](./scenarios/) exercise **human-in-the-loop (HITL)** approval, policy variants, dispatch edge cases, and manifest validation. Run scenarios use the dev-only **`stub`** model provider (scripted tool calls via `stub-script.json`) so e2e tests do not need `OPENAI_API_KEY`.

## Prerequisites

- Go 1.25+
- Phrony runtime running with the e2e compose overlay (see below)
- `make -C .. build` so `../bin/phrony` exists (or set `PHRONY_BIN`)

## Runtime stack (e2e)

[`docker-compose.e2e.yml`](./docker-compose.e2e.yml) extends the [runtime dev stack](../docker-compose.yml) with settings required for scenarios and integration tests:

- `RUNTIME_ENABLE_STUB_PROVIDER=true` — scripted `stub` model provider (no `OPENAI_API_KEY`)
- `RUNTIME_DISPATCH_QUEUE_WAIT=10s` — fail queued tool calls when no worker is connected (F2/F3 paths)

From this directory:

```bash
make dev-up
# or: docker compose -f ../docker-compose.yml -f docker-compose.e2e.yml up -d --build --wait
```

From the runtime repo root:

```bash
make test-e2e-up
# or: docker compose -f docker-compose.yml -f e2e/docker-compose.e2e.yml up -d --build --wait
```

Tear down: `make dev-down` (here) or `make test-e2e-down` (repo root).

`make dev-up` in the runtime repo uses only the base compose file. For a host-run runtime (Postgres in Docker, process on the host), use `make serve-e2e` from the repo root — it loads `.env` then forces `RUNTIME_ENABLE_STUB_PROVIDER=true` and `RUNTIME_DISPATCH_QUEUE_WAIT=10s` (same as the compose overlay).

## Run (manual)

```bash
cp .env.example .env
set -a && source .env && set +a

make run
# or: go run ./cmd/worker
```

In another terminal, start the runtime with the e2e overlay:

```bash
make dev-up
```

Publish and run the baseline HITL scenario:

```bash
phrony validate scenarios/00-baseline-hitl/agent.yaml
phrony publish scenarios/00-baseline-hitl/agent.yaml
phrony deploy demo/payment-agent-baseline@1.0.1
phrony run demo/payment-agent-baseline --attach --input '{"message":"Pay 1500 USD to Acme Corp"}'
```

When the model proposes `process_payment` with `amount` greater than 1000, the session moves to `awaiting_approval`.

```bash
phrony approvals list
phrony approvals approve <approval-id> --comment "verified"
```

Policy for baseline: [`scenarios/00-baseline-hitl/policies/payment-require-approval.yaml`](./scenarios/00-baseline-hitl/policies/payment-require-approval.yaml) (`amount` > 1000 → `require_approval` on `tool:payments.process-payment`).

## Scenario index

| Directory | Agent | Purpose |
|-----------|-------|---------|
| `00-baseline-hitl` | `demo/payment-agent-baseline` | HITL when amount > 1000 |
| `01-auto-dispatch-low` | `demo/payment-agent-auto` | Auto dispatch ≤ 1000 |
| `16-archive-agent` | `demo/payment-agent-archive-e2e` | I1 only — archived at end of e2e |
| `02-deny-block` | `demo/payment-agent-deny` | Deny when amount > 500 |
| `03-allowlist-currency` | `demo/payment-agent-currency` | Allow USD only |
| `04-quorum-approval` | `demo/payment-agent-quorum` | Two approvers when amount > 1000 |
| `05-on-modify-revalidate` | `demo/payment-agent-revalidate` | `on_modify: revalidate` |
| `06-dispatch-escalate-no-handler` | `demo/payment-agent-no-handler` | Escalate on `dispatch:no_handler` |
| `07-wrong-tool-version` | `demo/payment-agent-bad-version` | Tool `@9.9.9` |
| `08-handler-validation` | `demo/payment-agent-validation` | Worker rejects invalid args |
| `10`–`14` | (various) | `phrony validate` negative fixtures |
| `15-version-bump` | `demo/payment-agent-bump` | Lifecycle rollback / deprecate |
| `17-validate-invalid-side-effect-class` | `demo/payment-agent-bad-side-effect` | Invalid `side_effect_class` on tool |
| `18-indeterminate-read-only` | `demo/payment-agent-indeterminate-readonly` | `read_only` + worker drop → tool error |
| `19-indeterminate-non-idempotent` | `demo/payment-agent-indeterminate-write` | `non_idempotent_write` + worker drop → HITL |
| `20-indeterminate-idempotent` | `demo/payment-agent-indeterminate-idempotent` | `idempotent_write` + worker drop → tool error |
| `21-indeterminate-irreversible` | `demo/payment-agent-indeterminate-irreversible` | `irreversible_action` + worker drop → HITL |
| `22-bundle-payment-auto` | `demo/payment-desk` | Bundle: orchestrator delegates payment ≤1000 to specialist |
| `23-bundle-payment-hitl` | `demo/payment-desk-hitl` | Bundle: delegation + HITL when specialist payment > 1000 |
| `24-bundle-delegation` | `demo/payment-routing` | Bundle: text-only sub-agent delegation (no tools) |
| `25-validate-bundle-no-lock` | `demo/payment-desk-unlocked` | J2 only — `bundle validate --require-lock` negative fixture |

Each run scenario includes `stub-script.json` (inlined at publish as `phrony.com/stub-script`). Bundle scenarios use `kind: Bundle` with committed `bundle.lock.json`; publish via `phrony bundle publish` and run via `phrony bundle run`.

## E2e tests

Validate-only (no runtime):

```bash
make test-e2e-validate
# or from repo root: make test-e2e-validate
```

Full integration suite (Postgres + runtime with stub provider + worker):

```bash
# Terminal 1 — runtime (e2e overlay)
make dev-up

# Terminal 2 — tests
make -C .. build
make test-e2e
```

From the repo root: `make test-e2e` (builds CLI, brings up e2e stack, runs the suite).

Against a host-run runtime (no compose restart):

```bash
# Terminal 1
docker compose up -d postgres --wait
make serve-e2e

# Terminal 2
make test-e2e-local
```

Tests call `phrony` for actions and gRPC for session/approval assertions. They **skip** (do not fail) when the runtime is unreachable.

**F1:** uses a **nodispatch** e2e worker (`PLAYGROUND_WORKER_MODE=nodispatch`) that declines `process_payment` without printing `payment processed:` — so the session does not sit in `awaiting_tool` for minutes. Stop any extra `make run` worker in another terminal; a stray real worker will still process the payment and break the test.

**F2 / F3 / worker-off paths:** require a **rebuilt runtime** with dispatch queue timeout (`RUNTIME_DISPATCH_QUEUE_WAIT`, default 10s; set in `docker-compose.e2e.yml`). Without it, sessions park in `awaiting_tool`. After pulling runtime changes: `make -C .. build` and restart the e2e compose stack.

**F6 / F7 (`side_effect_class`):** use an **indeterminate** e2e worker (`PLAYGROUND_WORKER_MODE=indeterminate`) that accepts `process_payment` then exits without sending a result, so the runtime records `dispatch:indeterminate`. `read_only` and `idempotent_write` finish the turn as a tool error; `non_idempotent_write` and `irreversible_action` escalate to `awaiting_approval`.

`make test-e2e` runs with **`E2E_LOG=1`** and **`-v`**. Go’s test runner **drops all output from passing tests** unless `-v` is set—so `E2E_LOG` lines would otherwise only appear when a test fails. With both flags, harness lines stream under each `=== RUN` as the test executes (including during long session polls).

Disable stderr narration: `E2E_LOG=0 make test-e2e` (you still get `-v` output after each test).

After the full run finishes, a **scenario overview** table is printed once (sanity id, PASS/FAIL/SKIP, test name, purpose).

## Handler

| Tool | Version | Behavior |
|------|---------|----------|
| `payments.process-payment` | `1.0.0` | Fake payment receipt for `{"amount":...,"currency":"...","payee":"..."}` |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PHRONY_RUNTIME_ADDR` | `127.0.0.1:7777` | Runtime gRPC address |
| `WORKER_ID` | `playground-worker-1` | Worker id on the Work stream |
| `WORKER_WORKLOAD_IDENTITY` | `playground/local` | Allowlist workload identity |
| `WORKER_IMAGE_DIGEST` | `sha256:playground-dev` | Allowlist image digest |
| `PHRONY_BIN` | `../bin/phrony` | CLI for e2e tests |

When the runtime sets `RUNTIME_TOOL_ALLOWLIST`, add matching `workload_identities` and `image_digests` for your agent and tools.

## Module layout

- `scenarios/` — agent and bundle manifests (`agent.yaml` or `bundle.yaml`, `policies/`, `tools/`, `stub-script.json`)
- `cmd/worker` — e2e tool worker entrypoint (not shipped in product binaries)
- `internal/workclient` — Work bidi stream client
- `internal/handlers` — playground tool implementations
- `e2e/` — integration tests and harness

This nested module depends on `github.com/phrony-platform/runtime` via `replace ../` for generated gRPC types.
