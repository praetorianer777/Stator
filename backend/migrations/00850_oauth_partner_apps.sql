-- +goose Up
-- Stator handing partner tools a person's token by token exchange (#461), as
-- Armature does since Cloudster1/Armature#280. A partner app is registered by
-- an administrator with the Keycloak client its tokens come from; a person's
-- access token of the shared realm, issued to that client, is traded for a
-- grant, which is an api_token row of the person's: it shows in their token
-- list as "<app> (connected app)" and is revoked there like any token.

CREATE TABLE oauth_client (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    name               text NOT NULL CONSTRAINT oauth_client_name_length CHECK (char_length(btrim(name)) BETWEEN 1 AND 60),
    -- read alone is a grant that may only read; write lets it do what its
    -- person may. read is always among them.
    scopes             text[] NOT NULL CONSTRAINT oauth_client_known_scopes
                       CHECK (scopes <@ ARRAY['read', 'write']::text[] AND 'read' = ANY (scopes)),
    keycloak_client_id text NOT NULL CONSTRAINT oauth_client_keycloak_named CHECK (btrim(keycloak_client_id) <> '')
                       CONSTRAINT oauth_client_keycloak_client_id_key UNIQUE,
    -- Only the SHA-256 of the stator_ocs_ secret, shown once.
    secret_hash        bytea NOT NULL CONSTRAINT oauth_client_secret_is_sha256 CHECK (octet_length(secret_hash) = 32),
    created_by         uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER oauth_client_set_updated_at BEFORE UPDATE ON oauth_client
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A grant: the token_hash is its current access token, refresh_hash its
-- refresh token, both rotated together; expires_at is when the refresh
-- token runs out unless used. One per person and app, gone with the app.
ALTER TABLE api_token
    ADD COLUMN client_id uuid REFERENCES oauth_client (id) ON DELETE CASCADE,
    ADD COLUMN refresh_hash bytea CONSTRAINT api_token_refresh_hash_key UNIQUE,
    ADD COLUMN access_expires_at timestamptz,
    ADD CONSTRAINT api_token_grant_whole CHECK (
        (client_id IS NULL AND refresh_hash IS NULL AND access_expires_at IS NULL)
        OR (client_id IS NOT NULL AND octet_length(refresh_hash) = 32 AND access_expires_at IS NOT NULL
            AND expires_at IS NOT NULL AND NOT spaces_only AND NOT for_render));

CREATE UNIQUE INDEX api_token_one_grant_idx ON api_token (user_id, client_id) WHERE client_id IS NOT NULL;

-- Refresh tokens already traded: one presented again ends its grant, as
-- Armature has no grace window either (RFC 9700 section 4.14.2).
CREATE TABLE oauth_rotated_refresh (
    refresh_hash bytea PRIMARY KEY CONSTRAINT oauth_rotated_refresh_is_sha256 CHECK (octet_length(refresh_hash) = 32),
    grant_id     uuid NOT NULL REFERENCES api_token (id) ON DELETE CASCADE,
    rotated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_rotated_refresh_grant_idx ON oauth_rotated_refresh (grant_id);

ALTER TABLE oauth_client ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_client FORCE  ROW LEVEL SECURITY;
CREATE POLICY oauth_client_app ON oauth_client USING (true) WITH CHECK (true);
CREATE POLICY oauth_client_admin_bypass ON oauth_client TO stator_admin USING (true) WITH CHECK (true);
-- Administrators keep the apps; the token endpoint reads them as the admin
-- role, since nobody is signed in there.
CREATE POLICY oauth_client_admins ON oauth_client AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));

ALTER TABLE oauth_rotated_refresh ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_rotated_refresh FORCE  ROW LEVEL SECURITY;
CREATE POLICY oauth_rotated_refresh_app ON oauth_rotated_refresh USING (true) WITH CHECK (true);
CREATE POLICY oauth_rotated_refresh_admin_bypass ON oauth_rotated_refresh TO stator_admin USING (true) WITH CHECK (true);

-- An anonymous reader names nobody, so none of them is theirs.
CREATE POLICY oauth_client_not_anonymous ON oauth_client AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY oauth_rotated_refresh_not_anonymous ON oauth_rotated_refresh AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- Only the token endpoint, as the admin role, makes or renews a grant; a
-- person may still read and revoke theirs, as any token of theirs.
CREATE POLICY api_token_no_forged_grant ON api_token AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (client_id IS NULL);
CREATE POLICY api_token_no_changed_grant ON api_token AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (client_id IS NULL) WITH CHECK (client_id IS NULL);

REVOKE ALL ON oauth_client, oauth_rotated_refresh FROM stator_app;
GRANT SELECT, DELETE ON oauth_client TO stator_app;
GRANT INSERT (name, scopes, keycloak_client_id, secret_hash, created_by) ON oauth_client TO stator_app;
GRANT UPDATE (name, scopes, keycloak_client_id, secret_hash) ON oauth_client TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_client, oauth_rotated_refresh TO stator_admin;

-- +goose Down
DROP POLICY IF EXISTS api_token_no_changed_grant ON api_token;
DROP POLICY IF EXISTS api_token_no_forged_grant ON api_token;
DROP TABLE IF EXISTS oauth_rotated_refresh;
DELETE FROM api_token WHERE client_id IS NOT NULL;
DROP INDEX IF EXISTS api_token_one_grant_idx;
ALTER TABLE api_token DROP CONSTRAINT IF EXISTS api_token_grant_whole,
    DROP COLUMN IF EXISTS access_expires_at,
    DROP COLUMN IF EXISTS refresh_hash,
    DROP COLUMN IF EXISTS client_id;
DROP TABLE IF EXISTS oauth_client;
