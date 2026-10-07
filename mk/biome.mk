# TypeScript and JavaScript formatting for web/, e2e/ and render/, by the root biome.json.
# Biome is not a dependency of any of them, so it runs from its own image,
# pinned to the version the editor tooling formats with.
BIOME_IMAGE ?= ghcr.io/biomejs/biome:2.5.8
BIOME_DIRS := web e2e render

DOCKER_BIOME = docker run --rm -u $(UID_GID) -v $(ROOT):/code -w /code $(BIOME_IMAGE)

.PHONY: check-format
check-format: ## Fail when a file in web/, e2e/ or render/ is not formatted as biome.json says
	@$(DOCKER_BIOME) format $(BIOME_DIRS) \
		|| { echo "Some files in $(BIOME_DIRS) are not formatted. Run make format, review the changes and commit them."; exit 1; }

.PHONY: format
format: ## Format web/, e2e/ and render/ in place as biome.json says
	$(DOCKER_BIOME) format --write $(BIOME_DIRS)
