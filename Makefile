.PHONY: help default \
        cluster/create cluster/destroy \
        build build/backend build/worker build/cli build/frontend \
				dbuild dbuild/backend dbuild/worker dbuild/cli \
				install/frontend \
				lint lint/go lint/frontend \
        clean clean/backend clean/worker clean/cli \
        compose/up compose/down \
        compose/auth/up compose/auth/down \
        compose/dev/up compose/dev/down \
        compose/dev/auth/up compose/dev/auth/down \
				helm/docs \
				generate/backend generate/mocks \
				bench bench/up bench/up-lower-case bench/down bench/correctness bench/perf bench/perf-up bench/pg-perf-up
default: help

help:
	@echo "Available commands:"
	@awk 'BEGIN {FS = ":.*##"; printf "\n"} /^[a-zA-Z_\/]+:.*##/ { printf "\033[36m%-30s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

PROD_COMPOSE_FILE = compose.yml
PROD_AUTH_COMPOSE_FILE = compose.auth.yml
DEV_COMPOSE_FILE = compose.dev.yml
DEV_AUTH_COMPOSE_FILE = compose.auth.yml

# Cluster Management
cluster/create: ## Creates a local K8s Cluster
	sh ./tilt/scripts/cluster-create.sh

cluster/destroy: ## Destroys a local K8s Cluster
	bash ./tilt/scripts/assert-context.sh
	sh ./tilt/scripts/cluster-destroy.sh

# Building
build: ## Builds the project (except the frontend)
	( \
	make build/backend & \
	make build/worker & \
	make build/cli & \
	make install/frontend & \
	wait \
	)

dbuild: ## Builds the project specifically for Linux
	( \
	make dbuild/backend & \
	make dbuild/worker & \
	make dbuild/cli & \
	make install/frontend & \
	wait \
	)

build/backend: ## Builds the backend
	@cd ./backend && make build

dbuild/backend: ## Runs the backend specifically for Linux
	@cd ./backend && make dbuild

build/worker: ## Builds the worker
	@cd ./worker && make build

dbuild/worker: ## Builds the worker specifically for Linux
	@cd ./worker && make dbuild

build/cli: ## Builds the CLI
	@cd ./cli && make build

dbuild/cli: ## Builds the CLI specifically for Linux
	@cd ./cli && make dbuild

install/frontend: ## Runs npm install for the frontend
	@cd ./frontend && npm install

build/frontend: ## Builds the frontend (don't do this if intending to develop locally)
	@cd ./frontend && npm run build

generate/backend: ## Runs the backend generate script
	@cd ./backend && make gen

generate/mocks: ## Regenerates the mocks declared in .mockery.yml (after generate/backend when an interface changed)
	mockery

# Linting
lint: ## Lints the project
	( \
	make lint/go & \
	make lint/frontend & \
	wait \
	)

lint/go: ## Lints the Go Module
	golangci-lint run

lint/frontend: ## Lints the frontend
	@cd ./frontend && npm run lint

# Cleaning
clean: ## Cleans the project
	( \
	make clean/backend & \
	make clean/worker & \
	make clean/cli & \
	wait \
	)

clean/backend: ## Cleans the backend
	@cd ./backend && make clean

clean/worker: ## Cleans the worker
	@cd ./worker && make clean

clean/cli: ## Cleans the CLI
	@cd ./cli && make clean

# Compose Management
compose/up: ## Pulls the latest images and stands up the production environment
	docker compose -f $(PROD_COMPOSE_FILE) pull
	BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose -f $(PROD_COMPOSE_FILE) up -d

compose/down: ## Composes down the production environment
	docker compose -f $(PROD_COMPOSE_FILE) down

compose/auth/up: ## Pulls the latest images and stands up the production environment with auth - Requires a valid Husonym Enterprise license!
	docker compose -f $(PROD_COMPOSE_FILE) -f $(PROD_AUTH_COMPOSE_FILE) pull
	BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose -f $(PROD_COMPOSE_FILE) -f $(PROD_AUTH_COMPOSE_FILE) up -d

compose/auth/down: ## Composes down the production environment with auth
	docker compose -f $(PROD_COMPOSE_FILE) -f $(PROD_AUTH_COMPOSE_FILE) down

compose/dev/up: ## Composes up the development environment.
	BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose -f $(DEV_COMPOSE_FILE) up -d

compose/dev/down: ## Composes down the development environment
	docker compose -f $(DEV_COMPOSE_FILE) down

compose/dev/auth/up: ## Composes up the development environment with auth. - Requires a valid Husonym Enterprise license!
	BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose -f $(DEV_COMPOSE_FILE) -f $(DEV_AUTH_COMPOSE_FILE) up -d

compose/dev/auth/down: ## Composes down the development environment with auth
	docker compose -f $(DEV_COMPOSE_FILE) -f $(DEV_AUTH_COMPOSE_FILE) down

# Engine test bench (plans/banc-essai-moteurs.md)
BENCH_COMPOSE_FILE = bench/compose.bench.yml
BENCH_SERVICES = bench-source bench-dest-benthos bench-dest-athanor
BENCH_PG_SERVICES = bench-pg-source bench-pg-dest-benthos bench-pg-dest-athanor

bench: bench/up bench/correctness ## Stands up the engine test bench and runs it

bench/up: ## Starts the bench MySQL servers and restarts the dev worker with a small page size
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d --wait $(BENCH_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d worker

bench/up-lower-case: ## Recreates the bench MySQL destinations folding table names to lower case (lower_case_table_names=1), the source as is; bench/up gives them back
	BENCH_DEST_LOWER_CASE_TABLE_NAMES=1 docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d --wait --force-recreate bench-dest-benthos bench-dest-athanor

bench/down: ## Removes the bench MySQL servers and gives the dev worker its page size back
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) rm -sfv $(BENCH_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) up -d worker

bench/correctness: ## Runs every bench case on both engines and checks them against their expectation
	go run ./bench/cmd/enginebench run

bench/pg-up: ## Starts the bench PostgreSQL servers and restarts the dev worker with a small page size
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d --wait $(BENCH_PG_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d worker

bench/pg-down: ## Removes the bench PostgreSQL servers and gives the dev worker its page size back
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) rm -sfv $(BENCH_PG_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) up -d worker

bench/pg-correctness: ## Runs the PostgreSQL cases on both engines and checks them against their expectation
	BENCH_DIALECT=postgres go run ./bench/cmd/enginebench run

bench/perf-up: ## Starts the bench MySQL servers with the worker at its normal page size, for measuring
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d --wait $(BENCH_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) up -d worker

bench/pg-perf-up: ## Starts the bench PostgreSQL servers with the worker at its normal page size, for measuring (then: BENCH_DIALECT=postgres go run ./bench/cmd/enginebench perf)
	docker compose -f $(DEV_COMPOSE_FILE) -f $(BENCH_COMPOSE_FILE) up -d --wait $(BENCH_PG_SERVICES)
	docker compose -f $(DEV_COMPOSE_FILE) up -d worker

bench/perf: bench/perf-up ## Measures both engines on the dataset at scale (durations, rows/s, memory)
	go run ./bench/cmd/enginebench perf

helm/docs: ## Generates documentation for the repository's helm charts.
	./scripts/gen-helmdocs.sh
