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
