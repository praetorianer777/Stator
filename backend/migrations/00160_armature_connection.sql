-- +goose Up
-- The Armature instance an organization connects (#27). One row per
-- organization: where people open Armature, the Armature organization every
-- stored token has to belong to, and the secret Armature signs webhooks with,
-- sealed with STATOR_SECRET_KEY and bound to the organization.
CREATE TABLE armature_connection (
    org_id          uuid PRIMARY KEY REFERENCES org(id) ON DELETE CASCADE,
    -- An origin: a scheme, a host and maybe a port, nothing after it. The
    -- service says why in words; this keeps raw SQL to the same shape.
    base_url        text NOT NULL,
    org_slug        text NOT NULL,
    -- Learned from the first token that checks out, by the service as the
    -- admin role, so nobody can pin it with raw SQL.
    armature_org_id uuid,
    webhook_secret  bytea,
    updated_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT armature_connection_base_url_origin CHECK (base_url ~ '^https?://[^/?#@[:space:]]+$'),
    CONSTRAINT armature_connection_org_slug_shape CHECK (org_slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$')
);

CREATE TRIGGER armature_connection_set_updated_at BEFORE UPDATE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE armature_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_connection FORCE  ROW LEVEL SECURITY;

CREATE POLICY armature_connection_tenant_isolation ON armature_connection
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_connection_admin_bypass ON armature_connection TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON armature_connection TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS armature_connection;
