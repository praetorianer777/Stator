-- +goose Up
-- Stator as a connected app of Armature (#428): a person connects their
-- account with one click, by Armature's authorization code flow with PKCE,
-- instead of pasting a personal access token.

-- The app Stator is in the connected Armature: registered by Stator itself,
-- pending until an Armature administrator allows it, or pasted by a Stator
-- administrator where Armature registers apps only by hand. refused holds
-- Armature's own sentence when it would not register Stator. The secret is
-- sealed with STATOR_SECRET_KEY and bound to the site, as the webhook
-- secret is, and goes with the connection.
CREATE TABLE armature_app (
    singleton     boolean PRIMARY KEY DEFAULT true CONSTRAINT armature_app_singleton CHECK (singleton)
                  REFERENCES armature_connection (singleton) ON DELETE CASCADE,
    status        text NOT NULL CONSTRAINT armature_app_status_known CHECK (status IN ('pending', 'active', 'refused')),
    client_id     text,
    client_secret bytea,
    registered_by text NOT NULL CONSTRAINT armature_app_registered_by_known CHECK (registered_by IN ('stator', 'admin')),
    scopes        text[] NOT NULL DEFAULT '{}',
    refusal       text,
    updated_by    uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT armature_app_refused_or_registered CHECK (
        (status = 'refused' AND client_id IS NULL AND client_secret IS NULL AND refusal IS NOT NULL)
        OR (status <> 'refused' AND client_id IS NOT NULL AND btrim(client_id) <> '' AND refusal IS NULL))
);

CREATE TRIGGER armature_app_set_updated_at BEFORE UPDATE ON armature_app
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A person's grant: what Armature's token endpoint answered for them, both
-- tokens sealed and bound to the site and the person as a stored personal
-- token is. It goes with their membership and with the app it was issued to.
-- Refreshing rotates both tokens in place, under a row lock, since a refresh
-- token presented twice ends the grant in Armature.
CREATE TABLE armature_grant (
    user_id             uuid PRIMARY KEY REFERENCES member (user_id) ON DELETE CASCADE,
    -- New with every grant stored, so a cache entry keyed by it is never read
    -- for another grant or another Armature identity behind it.
    id                  uuid NOT NULL DEFAULT uuidv7() CONSTRAINT armature_grant_id_key UNIQUE,
    app                 boolean NOT NULL DEFAULT true CONSTRAINT armature_grant_app_one CHECK (app)
                        REFERENCES armature_app (singleton) ON DELETE CASCADE,
    access_token        bytea NOT NULL,
    refresh_token       bytea NOT NULL,
    access_expires_at   timestamptz NOT NULL,
    refresh_expires_at  timestamptz NOT NULL,
    scopes              text[] NOT NULL,
    armature_user_id    uuid,
    armature_user_name  text,
    armature_user_email text,
    status              text NOT NULL CONSTRAINT armature_grant_status_known CHECK (status IN ('ok', 'rejected', 'unreachable')),
    checked_at          timestamptz NOT NULL DEFAULT now(),
    refreshed_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now()
);

-- One trip to Armature's consent page: the state is answered only to the
-- session that started it, and only once. The digest is kept, never the
-- state, so a reader of the table cannot finish somebody's trip.
CREATE TABLE armature_oauth_attempt (
    state_digest  bytea PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES member (user_id) ON DELETE CASCADE,
    session_id    uuid NOT NULL,
    code_verifier text NOT NULL,
    return_to     text NOT NULL DEFAULT '',
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT armature_oauth_attempt_return_path CHECK (return_to = '' OR (return_to LIKE '/%' AND return_to NOT LIKE '//%'))
);

CREATE INDEX armature_oauth_attempt_expiry_idx ON armature_oauth_attempt (expires_at);

ALTER TABLE armature_app ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_app FORCE  ROW LEVEL SECURITY;
CREATE POLICY armature_app_app ON armature_app USING (true) WITH CHECK (true);
CREATE POLICY armature_app_admin_bypass ON armature_app TO stator_admin USING (true) WITH CHECK (true);
-- Administrators see and keep the app; what a member's connect needs of it,
-- the service reads as the admin role, so the sealed secret stays behind.
CREATE POLICY armature_app_admins ON armature_app AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));

ALTER TABLE armature_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_grant FORCE  ROW LEVEL SECURITY;
CREATE POLICY armature_grant_app ON armature_grant USING (true) WITH CHECK (true);
CREATE POLICY armature_grant_admin_bypass ON armature_grant TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY armature_grant_own ON armature_grant AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));

ALTER TABLE armature_oauth_attempt ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_oauth_attempt FORCE  ROW LEVEL SECURITY;
CREATE POLICY armature_oauth_attempt_app ON armature_oauth_attempt USING (true) WITH CHECK (true);
CREATE POLICY armature_oauth_attempt_admin_bypass ON armature_oauth_attempt TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY armature_oauth_attempt_own ON armature_oauth_attempt AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));

-- An anonymous reader names nobody, so none of them is theirs.
CREATE POLICY armature_app_not_anonymous ON armature_app AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY armature_grant_not_anonymous ON armature_grant AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY armature_oauth_attempt_not_anonymous ON armature_oauth_attempt AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- A grant's id is always a fresh default, so a new consent is a new row and
-- its cache is new too; a refresh rewrites the tokens of the row it locked.
REVOKE ALL ON armature_app, armature_grant, armature_oauth_attempt FROM stator_app;
GRANT SELECT, DELETE ON armature_app TO stator_app;
GRANT INSERT (status, client_id, client_secret, registered_by, scopes, refusal, updated_by) ON armature_app TO stator_app;
GRANT UPDATE (status, client_id, client_secret, registered_by, scopes, refusal, updated_by) ON armature_app TO stator_app;
GRANT SELECT, DELETE ON armature_grant TO stator_app;
GRANT INSERT (user_id, access_token, refresh_token, access_expires_at, refresh_expires_at, scopes,
              armature_user_id, armature_user_name, armature_user_email, status, checked_at)
    ON armature_grant TO stator_app;
GRANT UPDATE (access_token, refresh_token, access_expires_at, refresh_expires_at, scopes,
              armature_user_id, armature_user_name, armature_user_email, status, checked_at, refreshed_at)
    ON armature_grant TO stator_app;
GRANT SELECT, INSERT, DELETE ON armature_oauth_attempt TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON armature_app, armature_grant, armature_oauth_attempt TO stator_admin;

-- A new address forgets the app with the tokens: its client and every grant
-- belong to the Armature it was registered at.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token;
        DELETE FROM armature_app;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION armature_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM (SELECT user_id FROM armature_token UNION SELECT user_id FROM armature_grant) connected)
           END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM armature_token)
           END
$$;

CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP TABLE IF EXISTS armature_oauth_attempt;
DROP TABLE IF EXISTS armature_grant;
DROP TABLE IF EXISTS armature_app;
