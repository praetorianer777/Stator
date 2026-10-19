# Exciter's API document, vendored for the exciter-stub's contract test, as
# Armature's is (mk/armature.mk). Updating it is how a change in Exciter
# reaches Stator: the tests then say what has to follow. Fetched in a
# container, like every tool here.
EXCITER_REPO ?= Cloudster1/Exciter
EXCITER_API_DIR := $(ROOT)/api/exciter

.PHONY: exciter-openapi
exciter-openapi: ## Vendor Exciter's api/openapi.json at a commit: make exciter-openapi REF=<sha or branch>
	@[ -n "$(REF)" ] || { echo "Name the Exciter commit to vendor: make exciter-openapi REF=<sha or branch>"; exit 1; }
	@mkdir -p $(EXCITER_API_DIR)
	@sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' \
		https://api.github.com/repos/$(EXCITER_REPO)/commits/$(REF)) \
		|| { echo "GitHub does not know $(REF) in $(EXCITER_REPO)."; exit 1; }; \
	docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$(EXCITER_REPO)/$$sha/api/openapi.json \
		> $(EXCITER_API_DIR)/openapi.json.tmp \
		&& mv $(EXCITER_API_DIR)/openapi.json.tmp $(EXCITER_API_DIR)/openapi.json \
		&& printf '%s %s api/openapi.json\n' '$(EXCITER_REPO)' "$$sha" > $(EXCITER_API_DIR)/SOURCE \
		&& echo "Vendored $(EXCITER_REPO) $$sha; run make check-go to see what the stub has to follow."
