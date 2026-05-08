SHELL := /bin/bash
.DEFAULT_GOAL := help

SERVICES := auth registry license insurance inspection fines audit anpr-gateway notifications gateway
APPS     := web-admin police-pwa web-citizen

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

go-test: ## Test every Go service
	@for s in $(SERVICES); do (cd services/$$s && go test ./...) || exit 1; done

# ─── Web ────────────────────────────────────────────────────────────────
web-install: ## pnpm install for all web apps
	@for a in $(APPS); do (cd apps/$$a && pnpm install) || exit 1; done

web: ## Run all Next.js apps in parallel
	@( cd apps/web-admin && pnpm dev -p 3000 ) & \
	 ( cd apps/police-pwa && pnpm dev -p 3001 ) & \
	 ( cd apps/web-citizen && pnpm dev -p 3002 ) & \
	 wait

# ─── Test / lint ────────────────────────────────────────────────────────
test: go-test ## Run all tests

fmt: ## Format Go + TS
	@for s in $(SERVICES); do (cd services/$$s && gofmt -w .) ; done
	@for a in $(APPS); do (cd apps/$$a && pnpm format 2>/dev/null || true); done

.PHONY: help up down logs ps migrate migrate-down seed psql \
        go-tidy go-build go-test web-install web test fmt
