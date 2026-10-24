-- +goose Up
-- Apps that connect to the MCP endpoint as a person by OAuth (#449), as
-- Armature's connected apps do since Cloudster1/Armature#280: an app
-- registers itself (RFC 7591) and waits, pending, until an administrator
-- allows it by connecting it once; then members connect it on a consent page
-- by authorization code with PKCE. The partner apps of #461 stay as they are.

ALTER TABLE oauth_client
    ADD COLUMN kind text NOT NULL DEFAULT 'partner'
        CONSTRAINT oauth_client_kind_known CHECK (kind IN ('partner', 'registered')),
    ADD COLUMN status text NOT NULL DEFAULT 'allowed'
        CONSTRAINT oauth_client_status_known CHECK (status IN ('pending', 'allowed')),
    ADD COLUMN redirect_uris text[] NOT NULL DEFAULT '{}',
    ADD COLUMN client_uri text NOT NULL DEFAULT '',
    -- none is a public client, which proves itself with PKCE alone.
    ADD COLUMN auth_method text NOT NULL DEFAULT 'client_secret_basic'
        CONSTRAINT oauth_client_auth_method_known CHECK (auth_method IN ('client_secret_basic', 'client_secret_post', 'none')),
    ALTER COLUMN keycloak_client_id DROP NOT NULL,
    ALTER COLUMN secret_hash DROP NOT NULL,
    -- A partner is registered by an administrator, with its Keycloak client
    -- and a secret; an app that registered itself names where codes go back.
    ADD CONSTRAINT oauth_client_kind_shape CHECK (
        (kind = 'partner' AND keycloak_client_id IS NOT NULL AND secret_hash IS NOT NULL
            AND status = 'allowed' AND auth_method <> 'none')
        OR (kind = 'registered' AND keycloak_client_id IS NULL AND cardinality(redirect_uris) BETWEEN 1 AND 5
            AND (auth_method = 'none') = (secret_hash IS NULL)));

CREATE INDEX oauth_client_pending_idx ON oauth_client (created_at) WHERE status = 'pending';

-- An authorization code, used once. spaces is NULL for a grant that reaches
-- every space its person does; a code used again ends the grant it made.
CREATE TABLE oauth_code (
    code_hash    bytea PRIMARY KEY CONSTRAINT oauth_code_is_sha256 CHECK (octet_length(code_hash) = 32),
    client_id    uuid NOT NULL REFERENCES oauth_client (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    scopes       text[] NOT NULL CONSTRAINT oauth_code_known_scopes
                 CHECK (scopes <@ ARRAY['read', 'write']::text[] AND 'read' = ANY (scopes)),
    spaces       uuid[],
    redirect_uri text NOT NULL,
    challenge    text NOT NULL,
    resource     text NOT NULL DEFAULT '',
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    grant_id     uuid REFERENCES api_token (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_code_expiry_idx ON oauth_code (expires_at);

-- A grant made by consent acts at the MCP endpoint only, and may be limited
-- to spaces as a personal token is.
ALTER TABLE api_token
    ADD COLUMN mcp_only boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT api_token_mcp_only_is_grant CHECK (NOT mcp_only OR client_id IS NOT NULL),
    DROP CONSTRAINT api_token_grant_whole,
    ADD CONSTRAINT api_token_grant_whole CHECK (
        (client_id IS NULL AND refresh_hash IS NULL AND access_expires_at IS NULL)
        OR (client_id IS NOT NULL AND octet_length(refresh_hash) = 32 AND access_expires_at IS NOT NULL
            AND expires_at IS NOT NULL AND NOT for_render));

-- Nothing an app asks for works before an administrator allowed it, whoever
-- writes the row.
-- +goose StatementBegin
CREATE FUNCTION oauth_client_allowed_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.client_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM oauth_client WHERE id = NEW.client_id AND status = 'allowed') THEN
        RAISE EXCEPTION 'a connected app is allowed by an administrator before anybody connects it'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'oauth_client_allowed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER api_token_client_allowed BEFORE INSERT OR UPDATE OF client_id ON api_token
    FOR EACH ROW EXECUTE FUNCTION oauth_client_allowed_guard();
CREATE TRIGGER oauth_code_client_allowed BEFORE INSERT OR UPDATE OF client_id ON oauth_code
    FOR EACH ROW EXECUTE FUNCTION oauth_client_allowed_guard();

-- Codes are made and redeemed by the admin role only: the consent page writes
-- one for the person it asked, the token endpoint reads it for an app.
ALTER TABLE oauth_code ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_code FORCE  ROW LEVEL SECURITY;
CREATE POLICY oauth_code_admin_bypass ON oauth_code TO stator_admin USING (true) WITH CHECK (true);
-- An anonymous reader names nobody, so none of them is theirs.
CREATE POLICY oauth_code_not_anonymous ON oauth_code AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
REVOKE ALL ON oauth_code FROM stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_code TO stator_admin;

-- An administrator allowing a pending app does so as themselves, under the
-- administrators-only policy oauth_client already has.
GRANT UPDATE (status) ON oauth_client TO stator_app;

-- +goose Down
REVOKE UPDATE (status) ON oauth_client FROM stator_app;
DROP TABLE IF EXISTS oauth_code;
DROP TRIGGER IF EXISTS api_token_client_allowed ON api_token;
DROP FUNCTION IF EXISTS oauth_client_allowed_guard();
DELETE FROM api_token WHERE client_id IS NOT NULL AND (mcp_only OR spaces_only);
ALTER TABLE api_token DROP CONSTRAINT IF EXISTS api_token_grant_whole,
    DROP CONSTRAINT IF EXISTS api_token_mcp_only_is_grant,
    DROP COLUMN IF EXISTS mcp_only,
    ADD CONSTRAINT api_token_grant_whole CHECK (
        (client_id IS NULL AND refresh_hash IS NULL AND access_expires_at IS NULL)
        OR (client_id IS NOT NULL AND octet_length(refresh_hash) = 32 AND access_expires_at IS NOT NULL
            AND expires_at IS NOT NULL AND NOT spaces_only AND NOT for_render));
DELETE FROM oauth_client WHERE kind = 'registered';
DROP INDEX IF EXISTS oauth_client_pending_idx;
ALTER TABLE oauth_client DROP CONSTRAINT IF EXISTS oauth_client_kind_shape,
    DROP COLUMN IF EXISTS auth_method,
    DROP COLUMN IF EXISTS client_uri,
    DROP COLUMN IF EXISTS redirect_uris,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS kind,
    ALTER COLUMN secret_hash SET NOT NULL,
    ALTER COLUMN keycloak_client_id SET NOT NULL;
