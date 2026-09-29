# The Helm chart. Helm runs in a container like every other toolchain.
HELM_IMAGE ?= alpine/helm:3.19.0
export HELM_IMAGE

.PHONY: test-helm
test-helm: ## Lint and render deploy/charts/stator for each database layout
	$(ROOT)/tests/test-helm.sh
