#!/usr/bin/env bash
# Reset the e2e Postgres database and restart the runtime (migrations run on boot).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE=(docker compose -f "${ROOT}/../docker-compose.yml" -f "${ROOT}/docker-compose.e2e.yml")

echo "e2e: ensuring postgres is up"
"${COMPOSE[@]}" up -d postgres --wait

echo "e2e: stopping runtime (release DB connections)"
"${COMPOSE[@]}" stop runtime 2>/dev/null || true

echo "e2e: dropping and recreating public schema"
"${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U phrony_runtime -d phrony_runtime <<'SQL'
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO phrony_runtime;
GRANT ALL ON SCHEMA public TO public;
SQL

echo "e2e: starting runtime (migrate on boot)"
"${COMPOSE[@]}" up -d --build --wait runtime

echo "e2e: database refreshed"
