# The Go backend. Every target runs the toolchain in a container as the invoking
# user, with module and build caches under .cache/ so they survive between runs.

# Bookworm rather than alpine: the race detector needs cgo, and alpine ships no
# C compiler. Pinned to 1.26; see CLAUDE.md.
GO_IMAGE ?= golang:1.26-bookworm
PG_IMAGE ?= postgres:18
GO_CACHE := $(ROOT)/.cache/go
GO_PKG   := github.com/praetorianer777/stator/backend
GO_LDFLAGS := -X $(GO_PKG)/internal/version.Version=$(VERSION)

# The whole checkout is mounted so the tests find api/openapi.json where a plain
# go test would, three levels above internal/httpapi.
go_run = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-v $(GO_CACHE):/cache \
	-e HOME=/tmp \
	-e GOCACHE=/cache/build \
	-e GOMODCACHE=/cache/mod \
	-e GOFLAGS=-buildvcs=false \
	-e GOTOOLCHAIN=local \
	$(1) \
	-w /work/backend $(GO_IMAGE)
DOCKER_GO = $(call go_run,)

# One database, network and port per checkout, derived from its path, so gates
# running in parallel worktrees never share a database or collide on a port.
DB_HASH := $(shell printf '%s' '$(ROOT)' | cksum | cut -d' ' -f1)
DB_NAME := stator-db-$(DB_HASH)
DB_NET  := stator-net-$(DB_HASH)
DB_PORT ?= $(shell echo $$((20000 + $(DB_HASH) % 10000)))
DB_READY_ATTEMPTS := 60

# Credentials for the throwaway database; nothing else ever connects to it.
DB_SUPER_PASSWORD ?= stator
DB_APP_PASSWORD   ?= stator_app
DB_ADMIN_PASSWORD ?= stator_admin
DB_URL = postgres://$(1):$(2)@$(DB_NAME):5432/stator?sslmode=disable

DOCKER_GO_DB = $(call go_run,--network $(DB_NET) \
	-e STATOR_DB_PRIMARY_URL='$(call DB_URL,stator_app,$(DB_APP_PASSWORD))' \
	-e STATOR_DB_ADMIN_URL='$(call DB_URL,stator_admin,$(DB_ADMIN_PASSWORD))' \
	-e STATOR_TEST_SUPERUSER_URL='$(call DB_URL,stator,$(DB_SUPER_PASSWORD))')

$(GO_CACHE):
	@mkdir -p $@

.PHONY: fmt
fmt: | $(GO_CACHE) ## Format the Go sources in place
	$(DOCKER_GO) gofmt -w .

.PHONY: vet
vet: | $(GO_CACHE) ## Run go vet over the backend
	$(DOCKER_GO) go vet ./...

.PHONY: test-go
test-go: | $(GO_CACHE) ## Run the Go unit tests with the race detector
	$(DOCKER_GO) go test -race $(TESTFLAGS) ./...

.PHONY: build-go
build-go: | $(GO_CACHE) ## Build every backend binary into backend/bin, stamped with VERSION
	$(DOCKER_GO) go build -trimpath -ldflags '$(GO_LDFLAGS)' -o bin/ ./cmd/...

.PHONY: tidy
tidy: | $(GO_CACHE) ## Run go mod tidy
	$(DOCKER_GO) go mod tidy

.PHONY: openapi
openapi: | $(GO_CACHE) ## Regenerate api/openapi.json from the route table
	$(DOCKER_GO) go run ./cmd/openapi ../api/openapi.json

.PHONY: openapi-check
openapi-check: | $(GO_CACHE) ## Fail when api/openapi.json differs from what the code generates
	@$(DOCKER_GO) sh -c 'go run ./cmd/openapi /tmp/openapi.json && diff -u ../api/openapi.json /tmp/openapi.json' \
		|| { echo "api/openapi.json is out of date. Run make openapi and commit the result."; exit 1; }

.PHONY: fmt-check
fmt-check: | $(GO_CACHE) ## Fail when a Go file is not gofmt-clean
	@$(DOCKER_GO) sh -c 'out=$$(gofmt -l .); [ -z "$$out" ] || { echo "These files need formatting; run make fmt:"; echo "$$out"; exit 1; }'

.PHONY: check-go
check-go: fmt-check vet test-go openapi-check ## The backend gate: formatting, vet, race-checked tests, OpenAPI drift

.PHONY: db-up
db-up: | $(GO_CACHE) ## Start this checkout's throwaway Postgres 18 and migrate it
	@docker network inspect $(DB_NET) >/dev/null 2>&1 || docker network create $(DB_NET) >/dev/null
	@docker inspect $(DB_NAME) >/dev/null 2>&1 || docker run -d --name $(DB_NAME) --network $(DB_NET) \
		-p 127.0.0.1:$(DB_PORT):5432 \
		--tmpfs /var/lib/postgresql \
		-e POSTGRES_USER=stator -e POSTGRES_DB=stator -e POSTGRES_PASSWORD='$(DB_SUPER_PASSWORD)' \
		-e APP_DB_PASSWORD='$(DB_APP_PASSWORD)' -e ADMIN_DB_PASSWORD='$(DB_ADMIN_PASSWORD)' \
		-v $(ROOT)/deploy/postgres/init-roles.sh:/docker-entrypoint-initdb.d/10-roles.sh:ro \
		$(PG_IMAGE) >/dev/null
	@# The image's first-boot server listens on the socket only, so asking over
	@# TCP waits for the real one, after the init scripts have run.
	@for i in $$(seq $(DB_READY_ATTEMPTS)); do \
		docker exec $(DB_NAME) pg_isready -q -h 127.0.0.1 -U stator -d stator && exit 0; sleep 0.5; \
	done; echo "Postgres did not come up; see docker logs $(DB_NAME)."; exit 1
	$(call go_run,--network $(DB_NET) -e STATOR_DB_PRIMARY_URL='$(call DB_URL,stator,$(DB_SUPER_PASSWORD))') go run ./cmd/migrate up
	@echo "Postgres for this checkout: 127.0.0.1:$(DB_PORT) (container $(DB_NAME))"

.PHONY: db-down
db-down: ## Remove this checkout's throwaway Postgres
	@docker rm -f $(DB_NAME) >/dev/null 2>&1 || true
	@docker network rm $(DB_NET) >/dev/null 2>&1 || true

# The database is removed afterwards unless KEEP_DB=1, so a run leaves nothing
# behind; keeping it saves the start-up while iterating.
.PHONY: test-integration
test-integration: db-up ## Run the integration suite against this checkout's Postgres
	@status=0; $(DOCKER_GO_DB) go test -race -tags integration -count=1 $(TESTFLAGS) ./test/... || status=$$?; \
	[ "$(KEEP_DB)" = 1 ] || $(MAKE) --no-print-directory db-down; exit $$status
