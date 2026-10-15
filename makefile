COMPOSE_PROJECT ?= codego-v3-dev
ENV_FILE ?= .env
DEV_WEB_UPSTREAM ?= http://127.0.0.1:18083
DEV_COMPOSE = docker compose --env-file "$(ENV_FILE)" -f v3/deploy/compose.test.yaml -p "$(COMPOSE_PROJECT)"

.PHONY: dev dev-web stop logs build verify verify-web

dev:
	$(DEV_COMPOSE) up --build -d

dev-web:
	cd web/app && bun install --frozen-lockfile && V3_CONTROL_URL="$(DEV_WEB_UPSTREAM)" V3_GATEWAY_URL="$(DEV_WEB_UPSTREAM)" bun run dev

stop:
	$(DEV_COMPOSE) down

logs:
	$(DEV_COMPOSE) logs --tail=100 -f

build:
	cd v3 && go build ./cmd/gateway ./cmd/control ./cmd/worker ./cmd/migrate

verify:
	bash v3/scripts/verify.sh boundaries test lint race integration pgtest atlas

verify-web:
	cd web/app && bun install --frozen-lockfile && bun run typecheck && bun run check:api && bun run test && bun run build
