# The Go backend. Every target runs the toolchain in a container as the invoking
# user, with module and build caches under .cache/ so they survive between runs.

# Bookworm rather than alpine: the race detector needs cgo, and alpine ships no
# C compiler. Pinned to 1.26; see CLAUDE.md.
GO_IMAGE ?= golang:1.26-bookworm
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

# The packages test-go runs; PKG=./internal/spaceio/... runs one while working on it.
PKG ?= ./...

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
	$(ROOT)/scripts/go-test.sh $(REPORTS)/go-unit.json $(DOCKER_GO) go test -race -json $(TESTFLAGS) $(PKG)

.PHONY: build-go
build-go: | $(GO_CACHE) ## Build every backend binary into backend/bin, stamped with VERSION
	$(DOCKER_GO) go build -trimpath -ldflags '$(GO_LDFLAGS)' -o bin/ ./cmd/...

.PHONY: tidy
tidy: | $(GO_CACHE) ## Run go mod tidy
	$(DOCKER_GO) go mod tidy

.PHONY: openapi
openapi: | $(GO_CACHE) ## Regenerate api/openapi.json from the route table
	$(DOCKER_GO) go run ./cmd/openapi ../api/openapi.json
	$(MAKE) --no-print-directory web-schema

.PHONY: openapi-check
openapi-check: | $(GO_CACHE) ## Fail when api/openapi.json differs from what the code generates
	@$(DOCKER_GO) sh -c 'go run ./cmd/openapi /tmp/openapi.json && diff -u ../api/openapi.json /tmp/openapi.json' \
		|| { echo "api/openapi.json is out of date. Run make openapi and commit the result."; exit 1; }

.PHONY: document-allowlist
document-allowlist: | $(GO_CACHE) ## Regenerate api/document-allowlist.json, api/comment-allowlist.json and api/template-allowlist.json from the Go allowlist
	$(DOCKER_GO) go run ./cmd/docallowlist ../api/document-allowlist.json ../api/comment-allowlist.json ../api/template-allowlist.json

.PHONY: document-allowlist-check
document-allowlist-check: | $(GO_CACHE) ## Fail when an allowlist in api/ differs from what the code generates
	@$(DOCKER_GO) sh -c 'go run ./cmd/docallowlist /tmp/document-allowlist.json /tmp/comment-allowlist.json /tmp/template-allowlist.json && diff -u ../api/document-allowlist.json /tmp/document-allowlist.json && diff -u ../api/comment-allowlist.json /tmp/comment-allowlist.json && diff -u ../api/template-allowlist.json /tmp/template-allowlist.json' \
		|| { echo "The allowlists in api/ are out of date. Run make document-allowlist and commit the result."; exit 1; }

.PHONY: fmt-check
fmt-check: | $(GO_CACHE) ## Fail when a Go file is not gofmt-clean
	@$(DOCKER_GO) sh -c 'out=$$(gofmt -l .); [ -z "$$out" ] || { echo "These files need formatting; run make fmt:"; echo "$$out"; exit 1; }'

.PHONY: check-go
check-go: fmt-check vet test-go openapi-check document-allowlist-check ## The backend gate: formatting, vet, race-checked tests, OpenAPI and allowlist drift

# The suite runs against the compose stack (mk/stack.mk) rather than a database
# of its own, so the gate never starts a second Postgres.
.PHONY: test-integration
test-integration: | $(GO_CACHE) ## Run the integration suite against this checkout's running stack
	@docker compose ps --status running --services 2>/dev/null | grep -qx api \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	$(ROOT)/scripts/go-test.sh $(REPORTS)/integration.json $(DOCKER_GO_STACK) go test -race -json -tags integration -count=1 $(TESTFLAGS) ./test/...
