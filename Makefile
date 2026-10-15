.PHONY: help generate build test lint clean docker-build compose-up compose-down smoke-test e2e demo-web seed e2e-web

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

generate: ## Generate all code (OpenAPI, sqlc, buf)
	@echo "==> Generating OpenAPI Go server code..."
	cd schemas/openapi && ~/go/bin/oapi-codegen -package gen -generate chi-server,models,embedded-spec -o ../../go/internal/api/gen/server.go api.yaml
	@echo "==> Generating TypeScript client..."
	cd packages/api-client && pnpm run generate
	@echo "==> Generation complete"

build: ## Build all Go services
	@echo "==> Building Go services..."
	cd go && go build -o bin/ingest-gateway ./cmd/ingest-gateway
	cd go && go build -o bin/stream-processor ./cmd/stream-processor
	cd go && go build -o bin/query-api ./cmd/query-api
	cd go && go build -o bin/alerting-worker ./cmd/alerting-worker
	cd go && go build -o bin/platform ./cmd/platform
	@echo "==> Building web app..."
	cd apps/web && pnpm run build
	@echo "==> Building SDK..."
	cd packages/sdk-browser && pnpm run build

test: ## Run all tests
	@echo "==> Running Go tests..."
	cd go && go test -v ./...
	@echo "==> Running TypeScript tests..."
	pnpm run test

lint: ## Run linters
	@echo "==> Linting Go code..."
	cd go && golangci-lint run
	@echo "==> Linting TypeScript code..."
	pnpm run lint

clean: ## Clean build artifacts
	rm -rf go/bin
	rm -rf apps/web/.next
	rm -rf packages/*/dist
	find . -name "node_modules" -type d -prune -exec rm -rf {} +

docker-build: ## Build Docker images
	docker build -f deploy/compose/Dockerfile.go -t pulse/ingest-gateway:latest --target ingest-gateway .
	docker build -f deploy/compose/Dockerfile.go -t pulse/stream-processor:latest --target stream-processor .
	docker build -f deploy/compose/Dockerfile.go -t pulse/query-api:latest --target query-api .
	docker build -f deploy/compose/Dockerfile.go -t pulse/alerting-worker:latest --target alerting-worker .
	docker build -f deploy/compose/Dockerfile.web -t pulse/web:latest .
	docker build -f workers/ai-worker/Dockerfile -t pulse/ai-worker:latest ./workers/ai-worker

compose-up: ## Start Docker Compose stack and wait until healthy
	cd deploy/compose && docker compose --profile demo up -d --build --wait

compose-down: ## Stop Docker Compose stack
	cd deploy/compose && docker compose down

smoke-test: ## Assert events, spans, errors, issues and trace trees end to end (stack must be running)
	DEMO_API_URL=http://localhost:8090 ./tools/smoke-test.sh

e2e: compose-up smoke-test ## Start the stack and run the smoke test

demo-web: ## Build and serve the browser demo on http://localhost:5173
	pnpm --filter @pulse/browser build && pnpm --filter @pulse/browser-demo build
	cd examples/browser-demo && PORT=5173 node server.mjs

seed: ## Drive the browser demo with headless browsers to create demo traffic (demo-web must be running)
	pnpm --filter @pulse/web seed

e2e-web: ## Run the Playwright UI tests against http://localhost:3000 (after seed)
	pnpm --filter @pulse/web e2e
