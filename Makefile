# Athenaeum — common commands (docs/09-development.md §5).
# Targets whose code does not exist yet fail with a clear message instead of being omitted.

SHELL := /bin/bash
MIGRATE_VERSION ?= v4.18.3
MIGRATE := go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)
# Load .env (if present) into the recipe shell.
ENVLOAD := set -a; [ -f .env ] && . ./.env; set +a;

define need
	@test -e $(1) || { echo "error: $(1) does not exist yet ($(2))"; exit 1; }
endef

.PHONY: up down db-migrate seed reindex api web lint test vet e2e ci compose

up: ## postgres, redis, ollama, whisper (+ api/web once their Dockerfiles exist)
	docker compose up -d --build

down: ## stop the stack, keep volumes (use `docker compose down -v` to wipe)
	docker compose down

db-migrate:
	$(call need,api/migrations,arrives in milestone M1)
	@$(ENVLOAD) $(MIGRATE) -path api/migrations -database "$$DATABASE_URL" up

seed:
	$(call need,api/cmd/seed,arrives in milestone M3)
	@$(ENVLOAD) cd api && go run ./cmd/seed --mocks=./mocks

reindex:
	$(call need,api/cmd/reindex,arrives in milestone M3)
	@$(ENVLOAD) cd api && go run ./cmd/reindex

api:
	$(call need,api/cmd/api,arrives in milestone M1)
	@$(ENVLOAD) cd api && go run ./cmd/api

web:
	$(call need,web/package.json,arrives in milestone M4)
	cd web && pnpm dev

vet:
	@if [ -f api/go.mod ]; then cd api && go vet ./...; else echo "api: no go.mod yet, skipping vet"; fi

lint:
	@if [ -f api/go.mod ]; then \
	  test -z "$$(cd api && gofmt -l .)" || { echo "gofmt needed:"; (cd api && gofmt -l .); exit 1; }; \
	  command -v golangci-lint >/dev/null || { echo "error: golangci-lint not installed (brew install golangci-lint)"; exit 1; }; \
	  (cd api && golangci-lint run ./...); \
	else echo "api: no go.mod yet, skipping"; fi
	@if [ -f web/package.json ]; then pnpm -C web lint; else echo "web: no package.json yet, skipping"; fi

test:
	@if [ -f api/go.mod ]; then cd api && go test ./...; else echo "api: no go.mod yet, skipping"; fi
	@if [ -f web/package.json ]; then pnpm -C web test; else echo "web: no package.json yet, skipping"; fi

e2e:
	$(call need,web/playwright.config.ts,arrives in milestone M4)
	cd web && pnpm exec playwright test

ci: lint vet test e2e ## local replica of CI

compose: ## prod-like full build/start including api + web containers
	$(call need,api/Dockerfile,arrives in milestone M1)
	$(call need,web/Dockerfile,arrives in milestone M4)
	docker compose --profile app up -d --build
