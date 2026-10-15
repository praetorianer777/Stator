-- +goose Up
-- An endpoint keeps the envelope version it was registered for (#412), as
-- Armature's do. A delivery keeps the one it was queued in, so its retries and
-- redeliveries carry the same version whatever the endpoint is on by then.
-- Every endpoint and delivery so far is version 1, the envelope as it was
-- before it had a number.
ALTER TABLE webhook_endpoint
    ADD COLUMN version integer NOT NULL DEFAULT 1,
    ADD CONSTRAINT webhook_endpoint_version_positive CHECK (version >= 1);

-- NOT VALID spares the log a scan under the lock: every row already there has
-- the default, and every write from now on is held to the check.
ALTER TABLE webhook_delivery
    ADD COLUMN version integer NOT NULL DEFAULT 1,
    ADD CONSTRAINT webhook_delivery_version_positive CHECK (version >= 1) NOT VALID;

-- The app role registers an endpoint for a version and reads it back; the log
-- is the worker's, which writes it as the admin role.
GRANT SELECT (version), INSERT (version), UPDATE (version) ON webhook_endpoint TO stator_app;

-- +goose Down
REVOKE SELECT (version), INSERT (version), UPDATE (version) ON webhook_endpoint FROM stator_app;
ALTER TABLE webhook_delivery DROP COLUMN IF EXISTS version;
ALTER TABLE webhook_endpoint DROP COLUMN IF EXISTS version;
