-- +goose Up
-- Connecting people to Exciter at sign-in (#462), as to Armature (#430):
-- Stator trades a person's access token of the shared identity provider for
-- an Exciter grant at Exciter's token endpoint, as the partner app an
-- Exciter administrator registered for Stator.

-- The app, as a Stator administrator stored it: its client id, and its
-- secret sealed with STATOR_SECRET_KEY and bound to the site. It goes with
-- the connection, and with a new address.
CREATE TABLE exciter_app (
    singleton     boolean PRIMARY KEY DEFAULT true CONSTRAINT exciter_app_singleton CHECK (singleton)
                  REFERENCES exciter_connection (singleton) ON DELETE CASCADE,
    client_id     text NOT NULL CONSTRAINT exciter_app_client_named CHECK (btrim(client_id) <> ''),
    client_secret bytea NOT NULL,
    updated_by    uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER exciter_app_set_updated_at BEFORE UPDATE ON exciter_app
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A grant is the person's exciter_token row with a refresh token: token is
-- its access token, both sealed, rotated in place under a row lock, since a
-- refresh token presented twice ends the grant in Exciter. Only the admin
-- role writes one, so a member still never rewrites a token in place. It
-- goes with the app it was issued to.
ALTER TABLE exciter_token
    ADD COLUMN refresh_token bytea,
    ADD COLUMN access_expires_at timestamptz,
    ADD COLUMN app boolean CONSTRAINT exciter_token_app_one CHECK (app) REFERENCES exciter_app (singleton) ON DELETE CASCADE,
    ADD CONSTRAINT exciter_token_grant_whole CHECK (
        (refresh_token IS NULL AND access_expires_at IS NULL AND app IS NULL)
        OR (refresh_token IS NOT NULL AND access_expires_at IS NOT NULL AND app));

-- Why the exchange did not connect a person when they last signed in, in
-- Exciter's words where it gave some, for their profile. Storing a token
-- forgets it.
CREATE TABLE exciter_sign_in (
    user_id    uuid PRIMARY KEY REFERENCES member (user_id) ON DELETE CASCADE,
    connection boolean NOT NULL DEFAULT true CONSTRAINT exciter_sign_in_connection_one CHECK (connection)
               REFERENCES exciter_connection (singleton) ON DELETE CASCADE,
    refusal    text NOT NULL CONSTRAINT exciter_sign_in_refusal_said CHECK (btrim(refusal) <> ''),
    refused_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE exciter_app ENABLE ROW LEVEL SECURITY;
ALTER TABLE exciter_app FORCE  ROW LEVEL SECURITY;
CREATE POLICY exciter_app_app ON exciter_app USING (true) WITH CHECK (true);
CREATE POLICY exciter_app_admin_bypass ON exciter_app TO stator_admin USING (true) WITH CHECK (true);
-- Administrators keep it; a member's sign-in and refresh read it as the
-- admin role, so the sealed secret stays behind.
CREATE POLICY exciter_app_admins ON exciter_app AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY exciter_app_not_anonymous ON exciter_app AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

ALTER TABLE exciter_sign_in ENABLE ROW LEVEL SECURITY;
ALTER TABLE exciter_sign_in FORCE  ROW LEVEL SECURITY;
CREATE POLICY exciter_sign_in_app ON exciter_sign_in USING (true) WITH CHECK (true);
CREATE POLICY exciter_sign_in_admin_bypass ON exciter_sign_in TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY exciter_sign_in_own ON exciter_sign_in AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));
CREATE POLICY exciter_sign_in_not_anonymous ON exciter_sign_in AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

REVOKE ALL ON exciter_app, exciter_sign_in FROM stator_app;
GRANT SELECT, DELETE ON exciter_app TO stator_app;
GRANT INSERT (client_id, client_secret, updated_by) ON exciter_app TO stator_app;
GRANT UPDATE (client_id, client_secret, updated_by) ON exciter_app TO stator_app;
GRANT SELECT, DELETE ON exciter_sign_in TO stator_app;
GRANT INSERT (user_id, refusal, refused_at) ON exciter_sign_in TO stator_app;
GRANT UPDATE (refusal, refused_at) ON exciter_sign_in TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON exciter_app, exciter_sign_in TO stator_admin;

-- A new address forgets the app and what the exchange said, with the tokens.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION exciter_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM exciter_token;
        DELETE FROM exciter_app;
        DELETE FROM exciter_sign_in;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION exciter_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM exciter_token;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP TABLE IF EXISTS exciter_sign_in;
DELETE FROM exciter_token WHERE refresh_token IS NOT NULL;
ALTER TABLE exciter_token DROP CONSTRAINT IF EXISTS exciter_token_grant_whole,
    DROP COLUMN IF EXISTS app,
    DROP COLUMN IF EXISTS access_expires_at,
    DROP COLUMN IF EXISTS refresh_token;
DROP TABLE IF EXISTS exciter_app;
