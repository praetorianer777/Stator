-- +goose Up
-- Outbound webhooks (#110), as Armature keeps them: an endpoint is an address,
-- a secret it was shown once, the topics it takes and a log of every attempt,
-- one row per attempt. Unlike Armature the secret is sealed with
-- STATOR_SECRET_KEY, and an endpoint names its owner, whose permissions every
-- payload is read with when it is sent.
CREATE TABLE webhook_endpoint (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name            text NOT NULL,
    url             text NOT NULL,
    -- Sealed for this organization and endpoint, so it opens nowhere else.
    secret_sealed   bytea NOT NULL,
    topics          text[] NOT NULL DEFAULT '{}',
    enabled         boolean NOT NULL DEFAULT true,
    -- The administrator who last saved it. Leaving the organization leaves
    -- it without one, and then nothing but a ping is sent.
    owner_id        uuid,
    -- Failed attempts since the last one delivered, and since when; the
    -- worker turns an endpoint off that has failed long enough.
    failures        integer NOT NULL DEFAULT 0,
    failing_since   timestamptz,
    disabled_reason text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, owner_id) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (owner_id),
    CONSTRAINT webhook_endpoint_name_shape CHECK (btrim(name) <> '' AND length(name) <= 100),
    CONSTRAINT webhook_endpoint_url_shape CHECK (url ~ '^https?://[^/?#@\s]+' AND length(url) <= 2000),
    CONSTRAINT webhook_endpoint_topics_known CHECK (
        topics <@ ARRAY['*', 'page.published', 'page.moved', 'page.deleted', 'comment.created']::text[]),
    CONSTRAINT webhook_endpoint_disabled_reason CHECK (disabled_reason IS NULL OR disabled_reason = 'failing'),
    CONSTRAINT webhook_endpoint_failures CHECK (failures >= 0)
);

CREATE UNIQUE INDEX webhook_endpoint_name_idx ON webhook_endpoint (org_id, lower(name));

CREATE TABLE webhook_delivery (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id          uuid NOT NULL,
    endpoint_id     uuid NOT NULL,
    event_id        uuid NOT NULL,
    topic           text NOT NULL,
    -- The event as the outbox held it: ids, never words. The words are read
    -- as the owner at each attempt, so a page closed since is not sent.
    event           jsonb NOT NULL,
    occurred_at     timestamptz NOT NULL,
    attempt         integer NOT NULL DEFAULT 1,
    -- A test or a redelivery, which an administrator asked for by hand and
    -- which is sent even while the endpoint is off.
    manual          boolean NOT NULL DEFAULT false,
    state           text NOT NULL DEFAULT 'pending',
    status          integer,
    error           text NOT NULL DEFAULT '',
    -- While pending, when it is due; a sender holds it with a lease ahead.
    next_attempt_at timestamptz,
    attempted_at    timestamptz,
    delivered_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, endpoint_id) REFERENCES webhook_endpoint (org_id, id) ON DELETE CASCADE,
    CONSTRAINT webhook_delivery_once UNIQUE (endpoint_id, event_id, attempt),
    CONSTRAINT webhook_delivery_attempt_positive CHECK (attempt > 0),
    CONSTRAINT webhook_delivery_state CHECK (state IN ('pending', 'delivered', 'failed', 'withheld', 'cancelled')),
    CONSTRAINT webhook_delivery_due CHECK ((state = 'pending') = (next_attempt_at IS NOT NULL)),
    CONSTRAINT webhook_delivery_event_object CHECK (jsonb_typeof(event) = 'object')
);

CREATE INDEX webhook_delivery_due_idx ON webhook_delivery (next_attempt_at) WHERE state = 'pending';
CREATE INDEX webhook_delivery_endpoint_idx ON webhook_delivery (endpoint_id, created_at DESC, id DESC);
CREATE INDEX webhook_delivery_created_idx ON webhook_delivery (created_at);

-- +goose StatementBegin
-- Whoever saves an endpoint is its owner, and the database says so rather
-- than the caller. The worker acts for nobody and keeps the owner, and so
-- does the owner's membership going, which clears the owner and nothing
-- else. Turning an endpoint back on starts its count of failures again.
CREATE FUNCTION webhook_endpoint_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_actor_id() IS NOT NULL
       AND (TG_OP = 'INSERT' OR NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id) THEN
        NEW.owner_id := current_actor_id();
        NEW.updated_at := now();
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.enabled AND NOT OLD.enabled THEN
            NEW.failures := 0;
            NEW.failing_since := NULL;
            NEW.disabled_reason := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER webhook_endpoint_stamp BEFORE INSERT OR UPDATE ON webhook_endpoint
    FOR EACH ROW EXECUTE FUNCTION webhook_endpoint_stamp();

ALTER TABLE webhook_endpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoint FORCE  ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery FORCE  ROW LEVEL SECURITY;

CREATE POLICY webhook_endpoint_tenant_isolation ON webhook_endpoint
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY webhook_delivery_tenant_isolation ON webhook_delivery
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY webhook_endpoint_admin_bypass ON webhook_endpoint TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY webhook_delivery_admin_bypass ON webhook_delivery TO stator_admin USING (true) WITH CHECK (true);

-- Webhooks are the organization's administrators' to see and keep, as in
-- Armature. The subquery runs the check once per statement.
CREATE POLICY webhook_endpoint_administrators ON webhook_endpoint AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_is_admin(current_actor_id())))
    WITH CHECK ((SELECT perm_is_admin(current_actor_id())));
CREATE POLICY webhook_delivery_administrators ON webhook_delivery AS RESTRICTIVE FOR SELECT TO stator_app
    USING ((SELECT perm_is_admin(current_actor_id())));

-- The app role never reads a sealed secret back, never names the owner or
-- the worker's counts, and never writes the log: the worker and a test or
-- redelivery write it as the admin role, which also opens the secret.
REVOKE ALL ON webhook_endpoint, webhook_delivery FROM stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_endpoint, webhook_delivery TO stator_admin;
GRANT SELECT (id, org_id, name, url, topics, enabled, owner_id, failures, failing_since, disabled_reason, created_at, updated_at),
      INSERT (id, org_id, name, url, secret_sealed, topics, enabled),
      UPDATE (name, url, secret_sealed, topics, enabled),
      DELETE
    ON webhook_endpoint TO stator_app;
GRANT SELECT ON webhook_delivery TO stator_app;

-- +goose Down
DROP TABLE IF EXISTS webhook_delivery;
DROP TABLE IF EXISTS webhook_endpoint;
DROP FUNCTION IF EXISTS webhook_endpoint_stamp();
