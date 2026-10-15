# Armature's API document, vendored for the armature-stub's contract test.
# Updating it is how a change in Armature reaches Stator: the test then says
# what the stub has to follow. Fetched in a container, like every tool here.
ARMATURE_REPO ?= Cloudster1/Armature
CURL_IMAGE ?= curlimages/curl:8.16.0
ARMATURE_API_DIR := $(ROOT)/api/armature

.PHONY: armature-openapi
armature-openapi: ## Vendor Armature's api/openapi.json at a commit: make armature-openapi REF=<sha or branch>
	@[ -n "$(REF)" ] || { echo "Name the Armature commit to vendor: make armature-openapi REF=<sha or branch>"; exit 1; }
	@mkdir -p $(ARMATURE_API_DIR)
	@sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' \
		https://api.github.com/repos/$(ARMATURE_REPO)/commits/$(REF)) \
		|| { echo "GitHub does not know $(REF) in $(ARMATURE_REPO)."; exit 1; }; \
	docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$(ARMATURE_REPO)/$$sha/api/openapi.json \
		> $(ARMATURE_API_DIR)/openapi.json.tmp \
		&& mv $(ARMATURE_API_DIR)/openapi.json.tmp $(ARMATURE_API_DIR)/openapi.json \
		&& printf '%s %s api/openapi.json\n' '$(ARMATURE_REPO)' "$$sha" > $(ARMATURE_API_DIR)/SOURCE \
		&& echo "Vendored $(ARMATURE_REPO) $$sha; run make check-go to see what the stub has to follow."

# A real Armature beside the stack, for the test-armature layer: built from
# the commit api/armature/SOURCE names, so the Armature the browser suite runs
# against is the one whose document the stub is held to. That file is the one
# place the commit is pinned, for Dynamo as much as for Stator, and make
# armature-openapi moves both together. ARMATURE_GIT may name a local clone
# that has the commit, which saves the download.
ARMATURE_GIT ?= https://github.com/$(ARMATURE_REPO).git
ARMATURE_REF = $(word 2,$(shell cat $(ARMATURE_API_DIR)/SOURCE))
ARMATURE_SRC := $(ROOT)/.cache/armature/src
GIT_IMAGE ?= alpine/git:v2.52.0
ARMATURE_GIT_FROM = $(if $(wildcard $(ARMATURE_GIT)/.git),/origin,$(ARMATURE_GIT))
DOCKER_GIT = docker run --rm -u $(UID_GID) -e HOME=/tmp -v $(ARMATURE_SRC):/src \
	$(if $(wildcard $(ARMATURE_GIT)/.git),-v $(abspath $(ARMATURE_GIT)):/origin:ro) -w /src $(GIT_IMAGE)
ARMATURE_COMPOSE = STACK_NET=$(STACK_NET) ARMATURE_SRC=$(ARMATURE_SRC) ARMATURE_REF=$(ARMATURE_REF) \
	docker compose -p $(STACK_PROJECT)-armature -f $(ROOT)/deploy/armature/docker-compose.yml
# Where the specs find Armature, as a browser opens it.
ARMATURE_URL = http://localhost:$(ARMATURE_PORT)
# Builds the images one after another: the first compiles Armature, the
# others find every layer but their last in the cache, where side by side all
# three would compile it under the classic builder.
ARMATURE_BUILD = for service in api migrate worker; do \
		docker image inspect stator-armature-$$service:$(ARMATURE_REF) >/dev/null 2>&1 \
			|| $(ARMATURE_COMPOSE) build --quiet armature-$$service || exit 1; \
	done

.PHONY: armature-src
armature-src: ## Check out the Armature commit api/armature/SOURCE pins under .cache/armature/src
	@mkdir -p $(ARMATURE_SRC)
	@[ "$$($(DOCKER_GIT) -c safe.directory='*' rev-parse HEAD 2>/dev/null)" = "$(ARMATURE_REF)" ] \
		|| { find $(ARMATURE_SRC) -mindepth 1 -delete \
			&& $(DOCKER_GIT) -c safe.directory='*' init -q \
			&& $(DOCKER_GIT) -c safe.directory='*' -c protocol.file.allow=always fetch -q --depth 1 $(ARMATURE_GIT_FROM) $(ARMATURE_REF) \
			&& $(DOCKER_GIT) -c safe.directory='*' -c advice.detachedHead=false checkout -q FETCH_HEAD; } \
		|| { echo "Could not check out Armature $(ARMATURE_REF) from $(ARMATURE_GIT). Fetch it there first, or leave ARMATURE_GIT unset to use GitHub."; exit 1; }

.PHONY: armature-up
armature-up: armature-src ## Build and start the pinned Armature on the running stack's network
	@docker network inspect $(STACK_NET) >/dev/null 2>&1 \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@$(ARMATURE_BUILD)
	$(ARMATURE_COMPOSE) up -d --wait --quiet-pull
	@echo "Armature   $(ARMATURE_URL) ($(ARMATURE_REPO) $(ARMATURE_REF))"

.PHONY: armature-down
armature-down: ## Remove the real Armature and its data
	$(ARMATURE_COMPOSE) down -v --remove-orphans

# A fresh Armature for every run, so the seed always starts from nothing; it
# is removed afterwards unless KEEP_ARMATURE is set, and its logs are kept
# under reports/ when a spec failed. No recipe line calls make, so make -n,
# which run-tests.sh asks whether the target exists, runs nothing.
.PHONY: test-armature
test-armature: armature-src ## Run the real-Armature specs against the running stack and a fresh pinned Armature: ONLY=<grep> KEEP_ARMATURE=1
	@[ -f $(STACK_ENV_FILE) ] && docker compose ps --status running --services 2>/dev/null | grep -qx web \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@mkdir -p $(E2E_NPM_CACHE) $(REPORTS)
	@$(ARMATURE_COMPOSE) down -v --remove-orphans >/dev/null 2>&1 || true
	@$(ARMATURE_BUILD)
	@$(ARMATURE_COMPOSE) up -d --wait --quiet-pull \
		&& $(DOCKER_PLAYWRIGHT) \
			sh -c '$(E2E_INSTALL) && npx tsc --noEmit && E2E_PHASE=armature npx playwright test --workers=1 $(E2E_ARGS)'; \
	rc=$$?; \
	[ $$rc -eq 0 ] || $(ARMATURE_COMPOSE) logs --no-color --tail=300 armature-api armature-worker > $(REPORTS)/armature.log 2>&1; \
	[ -n "$(KEEP_ARMATURE)" ] || $(ARMATURE_COMPOSE) down -v --remove-orphans >/dev/null 2>&1; \
	exit $$rc
