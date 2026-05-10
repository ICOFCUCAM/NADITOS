SHELL := /bin/bash
.DEFAULT_GOAL := help

SERVICES := auth registry license insurance inspection fines audit anpr-gateway notifications gateway
APPS     := web-admin police-pwa web-citizen web-inspection web-insurance web-compliance

# ─── Help ───────────────────────────────────────────────────────────────
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

# ─── Local dev stack ────────────────────────────────────────────────────
up: ## Start postgres + redis + all services
	docker compose up -d --build

down: ## Stop everything
	docker compose down

logs: ## Tail all logs
	docker compose logs -f --tail=100

ps: ## Show running services
	docker compose ps

# ─── Database ───────────────────────────────────────────────────────────
migrate: ## Apply all migrations
	@./scripts/migrate.sh up

migrate-down: ## Rollback last migration
	@./scripts/migrate.sh down

seed: ## Seed demo tenant, users, vehicles
	@./scripts/seed.sh

psql: ## Open psql shell
	docker compose exec postgres psql -U naditos -d naditos

# ─── Go ─────────────────────────────────────────────────────────────────
go-tidy: ## Run go mod tidy in every service
	@for s in $(SERVICES); do (cd services/$$s && go mod tidy) || exit 1; done
	@(cd packages/go-common && go mod tidy)

go-build: ## Build every Go service
	@for s in $(SERVICES); do (cd services/$$s && go build -o ../../bin/$$s ./cmd/server) || exit 1; done

go-test: ## Test every Go package and service (-race, -count=1, with TEST_DATABASE_URL)
	@(cd packages/go-common && go test ./... -race -count=1) || exit 1
	@for s in $(SERVICES); do (cd services/$$s && go test ./... -race -count=1) || exit 1; done

go-vet: ## Vet every Go package and service
	@(cd packages/go-common && go vet ./...) || exit 1
	@for s in $(SERVICES); do (cd services/$$s && go vet ./...) || exit 1; done

# ─── Web ────────────────────────────────────────────────────────────────
web-install: ## pnpm install for the workspace
	@pnpm install --frozen-lockfile

web-build: ## pnpm build every Next.js app (matches CI's web job)
	@pnpm -r build

web-typecheck: ## pnpm type-check every Next.js app (matches CI's web job)
	@pnpm -r type-check

web: ## Run all Next.js apps in parallel (dev mode)
	@( cd apps/web-admin && pnpm dev -p 3000 ) & \
	 ( cd apps/police-pwa && pnpm dev -p 3001 ) & \
	 ( cd apps/web-citizen && pnpm dev -p 3002 ) & \
	 wait

# ─── Test / lint ────────────────────────────────────────────────────────
test: go-test ## Run all Go tests (-race)

smoke: ## Run the end-to-end smoke (14 stages, ~30s)
	@./scripts/smoke.sh

# `make check` is the local equivalent of CI's pre-merge gate. Run it
# before opening a PR — if it's green here it'll be green in GitHub
# Actions, so reviewers don't have to wait for CI to fail to ask you
# to fix something. Skips the integration / smoke jobs because those
# need a real Postgres; pair with `make up && make test` for those.
check: go-vet go-build web-install web-typecheck web-build ## Run the same checks CI runs (no DB)
	@echo "✓ all checks passed"

fmt: ## Format Go + TS
	@for s in $(SERVICES); do (cd services/$$s && gofmt -w .) ; done
	@for a in $(APPS); do (cd apps/$$a && pnpm format 2>/dev/null || true); done

.PHONY: help up down logs ps migrate migrate-down seed psql \
        go-tidy go-build go-test go-vet \
        web-install web-build web-typecheck web \
        test smoke check fmt
