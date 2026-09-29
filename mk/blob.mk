# A throwaway S3 compatible bucket for the integration suite: SeaweedFS, the
# store Armature runs. One container and port per checkout, derived from its
# path like the database's, on the database's network so the test container
# reaches it by name. The compose stack will serve the same purpose once it
# exists; until then this is the bucket the suite writes theme files to.
BLOB_IMAGE ?= chrislusf/seaweedfs:4.46
BLOB_NAME   = stator-blob-$(DB_HASH)
BLOB_PORT  ?= $(shell echo $$((30000 + $$(printf %s '$(ROOT)' | cksum | cut -d' ' -f1) % 10000)))
BLOB_READY_ATTEMPTS := 120

# Credentials for the throwaway bucket; nothing else ever connects to it.
BLOB_ACCESS_KEY ?= stator
BLOB_SECRET_KEY ?= stator-test-secret
BLOB_BUCKET     ?= stator-test

# What the integration suite is handed to reach the bucket.
INTEGRATION_ENV += -e STATOR_S3_ENDPOINT='$(BLOB_NAME):8333' \
	-e STATOR_S3_BUCKET='$(BLOB_BUCKET)' \
	-e STATOR_S3_ACCESS_KEY='$(BLOB_ACCESS_KEY)' \
	-e STATOR_S3_SECRET_KEY='$(BLOB_SECRET_KEY)'
INTEGRATION_DOWN += blob-down

.PHONY: blob-up
blob-up: ## Start this checkout's throwaway SeaweedFS S3 bucket
	@# The bucket and the database may start in parallel; either makes the network.
	@docker network inspect $(DB_NET) >/dev/null 2>&1 || docker network create $(DB_NET) >/dev/null 2>&1 \
		|| docker network inspect $(DB_NET) >/dev/null
	@docker inspect $(BLOB_NAME) >/dev/null 2>&1 || docker run -d --name $(BLOB_NAME) --network $(DB_NET) \
		-p 127.0.0.1:$(BLOB_PORT):8333 \
		--tmpfs /data \
		-e AWS_ACCESS_KEY_ID='$(BLOB_ACCESS_KEY)' -e AWS_SECRET_ACCESS_KEY='$(BLOB_SECRET_KEY)' \
		$(BLOB_IMAGE) server -dir=/data -s3 -s3.port=8333 -master.volumeSizeLimitMB=256 >/dev/null
	@# The master says the cluster is up; an answer on the S3 port, where a 403
	@# to an unsigned request is the healthy one, says the gateway is too.
	@for i in $$(seq $(BLOB_READY_ATTEMPTS)); do \
		docker exec $(BLOB_NAME) sh -c 'curl -fsS http://localhost:9333/cluster/healthz >/dev/null && curl -s -o /dev/null http://localhost:8333/' 2>/dev/null && exit 0; sleep 0.5; \
	done; echo "SeaweedFS did not come up; see docker logs $(BLOB_NAME)."; exit 1
	@echo "S3 bucket for this checkout: 127.0.0.1:$(BLOB_PORT) (container $(BLOB_NAME))"

.PHONY: blob-down
blob-down: ## Remove this checkout's throwaway SeaweedFS
	@docker rm -f $(BLOB_NAME) >/dev/null 2>&1 || true

test-integration: blob-up
