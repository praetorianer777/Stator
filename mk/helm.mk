# The Helm chart. Helm runs in a container like every other toolchain.
HELM_IMAGE ?= alpine/helm:3.19.0
export HELM_IMAGE

.PHONY: test-helm
test-helm: ## Lint and render deploy/charts/stator for each database layout
	$(ROOT)/tests/test-helm.sh

# The one subchart is fetched into the chart, and what it fetched is committed:
# see Chart.yaml for why. The chart directory is mounted writable for this.
.PHONY: helm-deps
helm-deps: ## Refresh the SeaweedFS subchart archive and Chart.lock after a change to Chart.yaml
	docker run --rm -u $(UID_GID) -e HOME=/tmp -v $(ROOT)/deploy/charts/stator:/chart -w /chart $(HELM_IMAGE) dependency update
