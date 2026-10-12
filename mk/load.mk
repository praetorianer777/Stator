# Load tests with k6 (tests/load, docs/load-testing.md) against this checkout's
# running stack, reached through nginx as a browser reaches it.

K6_IMAGE ?= grafana/k6:1.3.0
LOAD_PROFILE ?= standard
LOAD_SCENARIOS ?= readers,editors,live

.PHONY: test-load
test-load: ## Load the running stack: LOAD_PROFILE=smoke, standard or ceiling; fails when a threshold breaks
	@docker compose ps --status running --services 2>/dev/null | grep -qx api \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@mkdir -p $(REPORTS)
	docker run --rm -u $(UID_GID) --network $(STACK_NET) \
		-v $(ROOT)/tests/load:/load:ro -v $(REPORTS):/reports \
		-e LOAD_PROFILE=$(LOAD_PROFILE) -e LOAD_SCENARIOS=$(LOAD_SCENARIOS) $(if $(LOAD_PAGES),-e LOAD_PAGES=$(LOAD_PAGES)) $(if $(LOAD_SKIP),-e LOAD_SKIP=$(LOAD_SKIP)) \
		$(K6_IMAGE) run --quiet --summary-export=/reports/load-$(LOAD_PROFILE).json /load/stator.js

# The comparison with the committed baseline (#381) reads the run's summary in
# a Node container; it needs nothing installed beyond the image.
DOCKER_LOAD_NODE = docker run --rm -u $(UID_GID) -e HOME=/tmp \
	-v $(ROOT)/tests/load:/load -v $(REPORTS):/reports:ro -w /load $(NODE_IMAGE) node compare.mjs

# The baseline holds the standard profile with every scenario, so these run
# exactly that whatever the caller set. A run whose thresholds break is still
# compared, so its table says how far off it is, and then fails.
LOAD_STANDARD = $(MAKE) --no-print-directory test-load LOAD_PROFILE=standard LOAD_SCENARIOS=readers,editors,live LOAD_SKIP= LOAD_PAGES=

.PHONY: load-compare
load-compare: ## Run the standard profile (LOAD_COMPARE_ONLY=1: take the last run) and compare it with tests/load/baseline.json; fails when a p95 or the failure rate regresses
	@$(if $(LOAD_COMPARE_ONLY),,$(LOAD_STANDARD); run=$$?;) \
		$(DOCKER_LOAD_NODE) compare /load/baseline.json /reports/load-standard.json; cmp=$$?; \
		exit $$(( $${run:-0} > cmp ? $${run:-0} : cmp ))

.PHONY: load-baseline
load-baseline: ## Run the standard profile and write its numbers to tests/load/baseline.json, for a change that means to move them
	@$(LOAD_STANDARD)
	$(DOCKER_LOAD_NODE) baseline /reports/load-standard.json /load/baseline.json
