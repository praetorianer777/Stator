# Installs the chart into a real cluster: kind makes one out of a container, so
# a machine needs only Docker. Heavy (it builds the backend and web images and
# starts about a dozen pods), so it is not part of ./run-tests.sh; CI runs it
# when something the chart installs changes. See scripts/chart-install-test.sh.
CHART_TOOLS_IMAGE ?= stator-chart-tools:local

.PHONY: chart-tools
chart-tools: ## Build the image with kind, helm and kubectl the install test runs in
	docker build -q -t $(CHART_TOOLS_IMAGE) $(ROOT)/scripts/kind >/dev/null

.PHONY: test-chart-install
test-chart-install: chart-tools ## Install and upgrade the chart in a throwaway kind cluster
	docker run --rm --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(ROOT):/work:ro \
		-e STATOR_BUILD_VERSION -e STATOR_BUILD_COMMIT \
		-e KEEP_CLUSTER -e CLUSTER=stator-chart-install-$$(printf '%s' $(ROOT) | cksum | cut -d' ' -f1) \
		$(CHART_TOOLS_IMAGE) /work/scripts/chart-install-test.sh

# The commit whose chart and images the CloudNativePG test installs before it
# upgrades to this checkout's: the merge base, until a chart is published.
BASE_REF ?= $(shell git -C $(ROOT) merge-base HEAD origin/main 2>/dev/null)

.PHONY: test-chart-cnpg
test-chart-cnpg: chart-tools ## Upgrade a seeded instance on CloudNativePG from BASE_REF to this checkout in kind
	@test -n "$(BASE_REF)" || { echo "Set BASE_REF to the commit to upgrade from: no merge base with origin/main was found." >&2; exit 1; }
	base=$$(mktemp -d) && trap 'rm -rf "$$base"' EXIT && \
	git -C $(ROOT) archive $(BASE_REF) | tar -x -C "$$base" && \
	docker run --rm --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(ROOT):/work:ro -v "$$base":/base:ro \
		-e STATOR_BUILD_VERSION -e STATOR_BUILD_COMMIT \
		-e BASE_VERSION="$$(tr -d '[:space:]' <"$$base/VERSION")+dev" \
		-e BASE_COMMIT="$$(git -C $(ROOT) rev-parse --short=7 $(BASE_REF))" \
		-e KEEP_CLUSTER -e CLUSTER=stator-chart-cnpg-$$(printf '%s' $(ROOT) | cksum | cut -d' ' -f1) \
		$(CHART_TOOLS_IMAGE) /work/scripts/chart-install-cnpg-test.sh
