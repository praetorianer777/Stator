# The compose stack. The project name and every published port derive from the
# checkout's path, so worktrees running their gates side by side never share a
# database or collide on a port. Every compose call in a recipe finds the file
# and the project through the exported variables below.

STACK_HASH := $(shell printf '%s' '$(ROOT)' | cksum | cut -d' ' -f1)
STACK_PROJECT := stator-$(STACK_HASH)
STACK_NET := $(STACK_PROJECT)_default

# Twenty ports per checkout between 20000 and 29999, below the kernel's
# ephemeral range so an outgoing connection never holds one of them.
STACK_PORT_SLOTS := 500
STACK_PORT_STRIDE := 20
STACK_PORT_BASE ?= $(shell echo $$((20000 + $(STACK_HASH) % $(STACK_PORT_SLOTS) * $(STACK_PORT_STRIDE))))
stack_port = $(shell echo $$(($(STACK_PORT_BASE) + $(1))))

WEB_PORT              := $(call stack_port,0)
API_PORT              := $(call stack_port,1)
KEYCLOAK_PORT         := $(call stack_port,2)
MAILPIT_PORT          := $(call stack_port,3)
POSTGRES_PORT         := $(call stack_port,4)
POSTGRES_REPLICA_PORT := $(call stack_port,5)
VALKEY_PORT           := $(call stack_port,6)
S3_PORT               := $(call stack_port,7)

# Credentials for the throwaway development stack; override in the environment
# for anything else. The compose file carries the same defaults.
POSTGRES_PASSWORD  ?= stator
APP_DB_PASSWORD    ?= stator_app
ADMIN_DB_PASSWORD  ?= stator_admin
S3_ACCESS_KEY      ?= stator
S3_SECRET_KEY      ?= stator-dev-secret
VALKEY_PASSWORD    ?= stator_valkey

# The suite writes to a bucket of its own, so what it leaves behind never shows
# up among the files of the running app.
STACK_TEST_BUCKET := stator-test

export COMPOSE_FILE := $(ROOT)/deploy/docker-compose.yml
export COMPOSE_PROJECT_NAME := $(STACK_PROJECT)
export WEB_PORT API_PORT KEYCLOAK_PORT MAILPIT_PORT POSTGRES_PORT POSTGRES_REPLICA_PORT VALKEY_PORT S3_PORT
export POSTGRES_PASSWORD APP_DB_PASSWORD ADMIN_DB_PASSWORD S3_ACCESS_KEY S3_SECRET_KEY VALKEY_PASSWORD

# What the stack publishes, for the browser suite and for people: a shell can
# source it, and so can a Playwright config.
STACK_ENV_FILE := $(ROOT)/.cache/stack.env

STACK_PG_URL = postgres://$(1):$(2)@$(3):5432/stator?sslmode=disable

# The Go toolchain container on the stack's network, with everything the
# integration suite reads. Reads go to the replica as they do in the running
# api, so the suite proves read-your-writes rather than stepping around it.
DOCKER_GO_STACK = $(call go_run,--network $(STACK_NET) \
	-e STATOR_DB_PRIMARY_URL='$(call STACK_PG_URL,stator_app,$(APP_DB_PASSWORD),postgres-primary)' \
	-e STATOR_DB_REPLICA_URLS='$(call STACK_PG_URL,stator_app,$(APP_DB_PASSWORD),postgres-replica)' \
	-e STATOR_DB_ADMIN_URL='$(call STACK_PG_URL,stator_admin,$(ADMIN_DB_PASSWORD),postgres-primary)' \
	-e STATOR_VALKEY_URL='redis://:$(VALKEY_PASSWORD)@valkey:6379/0' \
	-e STATOR_TEST_SUPERUSER_URL='$(call STACK_PG_URL,stator,$(POSTGRES_PASSWORD),postgres-primary)' \
	-e STATOR_TEST_REPLICA_SUPERUSER_URL='$(call STACK_PG_URL,stator,$(POSTGRES_PASSWORD),postgres-replica)' \
	-e STATOR_S3_ENDPOINT=seaweedfs:8333 \
	-e STATOR_S3_BUCKET=$(STACK_TEST_BUCKET) \
	-e STATOR_S3_ACCESS_KEY='$(S3_ACCESS_KEY)' \
	-e STATOR_S3_SECRET_KEY='$(S3_SECRET_KEY)' \
	-e STATOR_TEST_WEB_URL=http://web \
	-e STATOR_TEST_KEYCLOAK_URL=http://keycloak:8080 \
	$(1))

.PHONY: stack-env
stack-env:
	@mkdir -p $(dir $(STACK_ENV_FILE))
	@printf '%s\n' \
		'COMPOSE_PROJECT_NAME=$(STACK_PROJECT)' \
		'STACK_NETWORK=$(STACK_NET)' \
		'WEB_PORT=$(WEB_PORT)' \
		'API_PORT=$(API_PORT)' \
		'KEYCLOAK_PORT=$(KEYCLOAK_PORT)' \
		'MAILPIT_PORT=$(MAILPIT_PORT)' \
		'POSTGRES_PORT=$(POSTGRES_PORT)' \
		'POSTGRES_REPLICA_PORT=$(POSTGRES_REPLICA_PORT)' \
		'VALKEY_PORT=$(VALKEY_PORT)' \
		'S3_PORT=$(S3_PORT)' \
		'STATOR_WEB_URL=http://localhost:$(WEB_PORT)' \
		'STATOR_KEYCLOAK_URL=http://localhost:$(KEYCLOAK_PORT)' \
		'STATOR_TEST_SUPERUSER_URL=postgres://stator:$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/stator?sslmode=disable' \
		> $(STACK_ENV_FILE)

.PHONY: up
up: stack-env ## Build and start the whole stack in the background, and say where it is
	docker compose up -d --build --wait
	@echo "Stator     http://localhost:$(WEB_PORT)"
	@echo "API        http://localhost:$(API_PORT)"
	@echo "Keycloak   http://localhost:$(KEYCLOAK_PORT) (admin / admin; realm stator-dev)"
	@echo "Mailpit    http://localhost:$(MAILPIT_PORT)"
	@echo "Postgres   127.0.0.1:$(POSTGRES_PORT) (replica $(POSTGRES_REPLICA_PORT))"
	@echo "Ports and project name are in $(STACK_ENV_FILE)."

.PHONY: down
down: ## Stop the stack, keeping its data
	docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)

.PHONY: logs
logs: ## Follow the stack's logs: make logs S=api for one service
	docker compose logs -f $(S)

.PHONY: seed
seed: ## Run the seed again against the running stack
	docker compose run --rm seed

.PHONY: shell
shell: | $(GO_CACHE) ## Interactive shell in the Go toolchain container, on the stack's network
	$(call DOCKER_GO_STACK,-it) sh

.PHONY: stack-up
stack-up: stack-env ## The gate's stack: build and start it, and wait until every service is healthy
	docker compose up -d --build --wait --quiet-pull

.PHONY: stack-down
stack-down: ## Remove the gate's stack and its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)
