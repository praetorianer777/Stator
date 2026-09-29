# The browser suite. It runs in Microsoft's Playwright image of exactly the
# version e2e/package.json pins, so the browsers in the image are the ones the
# library expects and nothing is downloaded at run time.
E2E_DIR := $(ROOT)/e2e
PLAYWRIGHT_VERSION := $(shell sed -n 's/.*"@playwright\/test": *"\([0-9][0-9.]*\)".*/\1/p' $(E2E_DIR)/package.json)
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v$(PLAYWRIGHT_VERSION)-noble
E2E_NPM_CACHE := $(ROOT)/.cache/npm

WORKERS ?= 4
ONLY ?=
E2E_REPORT_PORT ?= 9323

# On the host's network rather than the stack's: the api trusts the session
# cookie only from http://localhost:$(WEB_PORT), and the sign-in sends the
# browser to Keycloak's published port, so the browser has to reach the stack
# exactly as a person at this machine does. --ipc=host because Chromium runs
# out of the default 64 MB of shared memory.
DOCKER_PLAYWRIGHT = docker run --rm --init --ipc=host --network host $(DOCKER_NODE_TTY) \
	-u $(UID_GID) \
	-v $(ROOT):/src \
	-v $(E2E_NPM_CACHE):/npmcache \
	-e npm_config_cache=/npmcache \
	-e npm_config_update_notifier=false \
	-e HOME=/tmp \
	-e CI \
	-e ONLY='$(ONLY)' \
	-w /src/e2e $(PLAYWRIGHT_IMAGE)

# npm ci only when the lock file changed since the last install, which keeps
# a rerun of one spec to a couple of seconds.
E2E_INSTALL = [ node_modules/.package-lock.json -nt package-lock.json ] || npm ci --no-audit --no-fund

.PHONY: e2e-npm
e2e-npm: ## Run an npm command in the Playwright container: make e2e-npm ARGS="install"
	@mkdir -p $(E2E_NPM_CACHE)
	$(DOCKER_PLAYWRIGHT) npm $(ARGS)

.PHONY: test-e2e
test-e2e: ## Run the browser suite against this checkout's running stack: ONLY=<grep> WORKERS=<n>
	@[ -f $(STACK_ENV_FILE) ] && docker compose ps --status running --services 2>/dev/null | grep -qx web \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@mkdir -p $(E2E_NPM_CACHE)
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx tsc --noEmit && npx playwright test --workers=$(WORKERS) $${ONLY:+--grep "$$ONLY"}'

.PHONY: e2e-report
e2e-report: ## Serve the last browser run's HTML report, traces included
	@echo "Report on http://localhost:$(E2E_REPORT_PORT)"
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx playwright show-report playwright-report --host 127.0.0.1 --port $(E2E_REPORT_PORT)'
