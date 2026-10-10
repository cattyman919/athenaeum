#!/usr/bin/env bash
# Prepares the stack Playwright runs against (docs/09 §8.1): infra containers, an isolated
# `athenaeum_e2e` database (migrated, seeded, reindexed with real Ollama embeddings) and a
# clean Redis rate-limit state. The API, web and stub servers are started by
# web/playwright.config.ts (webServer). Used by `make e2e` and CI.
set -euo pipefail

cd "$(dirname "$0")/.."

E2E_DB="${E2E_DB:-athenaeum_e2e}"
export DATABASE_URL="postgres://athenaeum:athenaeum@localhost:5432/${E2E_DB}?sslmode=disable"
MIGRATE_VERSION="${MIGRATE_VERSION:-v4.18.3}"

echo "==> starting postgres, redis, ollama"
docker compose up -d postgres redis ollama

echo "==> waiting for postgres"
for _ in $(seq 1 60); do
  if docker compose exec -T postgres pg_isready -U athenaeum >/dev/null 2>&1; then break; fi
  sleep 1
done

echo "==> ensuring database ${E2E_DB}"
exists=$(docker compose exec -T postgres psql -U athenaeum -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${E2E_DB}'")
if [ "${exists}" != "1" ]; then
  docker compose exec -T postgres psql -U athenaeum -d postgres -c "CREATE DATABASE ${E2E_DB}"
fi

echo "==> migrating (down → up for a clean schema)"
migrate() {
  go run -tags postgres "github.com/golang-migrate/migrate/v4/cmd/migrate@${MIGRATE_VERSION}" \
    -path api/migrations -database "${DATABASE_URL}" "$@"
}
docker compose exec -T postgres psql -U athenaeum -d "${E2E_DB}" -v ON_ERROR_STOP=1 -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
migrate up

echo "==> ensuring Ollama embedding model"
if ! docker compose exec -T ollama ollama list | grep -q "nomic-embed-text"; then
  docker compose exec -T ollama ollama pull nomic-embed-text
fi

echo "==> seeding (offline) and reindexing"
(cd api && OFFLINE_MODE=true go run ./cmd/seed --mocks=./mocks --reset --offline)
(cd api && OFFLINE_MODE=true go run ./cmd/reindex)

echo "==> clearing rate-limit counters"
# Scan + DEL inside the container: a host-side `while read` loop would let
# `docker compose exec` swallow the remaining keys from stdin.
docker compose exec -T redis sh -c "redis-cli --scan --pattern 'rl:*' | xargs -r redis-cli DEL >/dev/null"

echo "==> e2e stack ready (database ${E2E_DB})"
