.PHONY: api worker web-install web-dev web-build infra-up infra-down migrate mode-precheck mode-upgrade check backup

api:
	cd server && go run ./cmd/api

worker:
	cd server && go run ./cmd/worker

web-install:
	cd web && corepack pnpm install

web-dev:
	cd web && corepack pnpm dev:ele

web-build:
	cd web && corepack pnpm build:ele

infra-up:
	docker compose -f deploy/compose.yaml up -d postgres redis objectstore

infra-down:
	docker compose -f deploy/compose.yaml down

migrate:
	docker compose -f deploy/compose.yaml exec -T postgres sh -c 'for migration in /migrations/*.sql; do echo "applying $$migration"; psql -v ON_ERROR_STOP=1 -U gin_dify -d gin_dify -f "$$migration"; done'

mode-precheck:
	cd server && APP_MODE=multi go run ./cmd/migrate-mode --to multi

mode-upgrade:
	cd server && APP_MODE=multi go run ./cmd/migrate-mode --to multi --apply

check:
	cd server && go test ./... && go vet ./...
	cd web && corepack pnpm --filter @vben/web-ele typecheck

backup:
	@test -n "$(BACKUP_DIR)" || (echo "BACKUP_DIR=/absolute/path is required" && exit 2)
	./deploy/scripts/backup.sh "$(BACKUP_DIR)"

