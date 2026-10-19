-- +goose Up
-- The Exciter installation the instance connects (#440), as Armature's is
-- kept (00160 to 00162): where people open Exciter, and the secret its
-- webhooks are signed with, sealed with STATOR_SECRET_KEY and bound to the
-- site. One row, like every setting of the one organization.
CREATE TABLE exciter_connection (
    singleton      boolean PRIMARY KEY DEFAULT true CONSTRAINT exciter_connection_singleton CHECK (singleton),
    -- An origin: a scheme, a host and maybe a port, nothing after it. The
    -- service says why in words; this keeps raw SQL to the same shape.
    base_url       text NOT NULL,
    webhook_secret bytea,
    updated_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exciter_connection_base_url_origin CHECK (base_url ~ '^https?://[^/?#@[:space:]]+$')
);

CREATE TRIGGER exciter_connection_set_updated_at BEFORE UPDATE ON exciter_connection
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A member's Exciter personal access token, sealed and bound to the site and
-- the person. Every read is made with the viewer's own, so a page shows a
-- requirement only to somebody Exciter shows it to.
CREATE TABLE exciter_token (
    user_id    uuid PRIMARY KEY REFERENCES member(user_id) ON DELETE CASCADE,
    -- New with every token stored, so a cache entry keyed by it is never
    -- read for another token or another Exciter identity behind it.
    id         uuid NOT NULL DEFAULT uuidv7() CONSTRAINT exciter_token_id_key UNIQUE,
    connection boolean NOT NULL DEFAULT true CONSTRAINT exciter_token_connection_one CHECK (connection)
               REFERENCES exciter_connection(singleton) ON DELETE CASCADE,
    token      bytea NOT NULL,
    -- Whom the token acts as in Exciter, as the last check found.
    exciter_user_id    uuid,
    exciter_user_name  text,
    exciter_user_email text,
    status     text NOT NULL CHECK (status IN ('ok', 'rejected', 'unreachable')),
    checked_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE exciter_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE exciter_connection FORCE  ROW LEVEL SECURITY;
ALTER TABLE exciter_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE exciter_token FORCE  ROW LEVEL SECURITY;

CREATE POLICY exciter_connection_app ON exciter_connection USING (true) WITH CHECK (true);
CREATE POLICY exciter_connection_admin_bypass ON exciter_connection TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY exciter_token_app ON exciter_token USING (true) WITH CHECK (true);
CREATE POLICY exciter_token_admin_bypass ON exciter_token TO stator_admin USING (true) WITH CHECK (true);

CREATE POLICY exciter_connection_not_anonymous ON exciter_connection AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY exciter_token_not_anonymous ON exciter_token AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- Administrators keep the connection; each member keeps their own token and
-- nobody reads anybody else's.
CREATE POLICY exciter_connection_admins ON exciter_connection AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY exciter_token_own ON exciter_token AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));

-- A new address forgets every stored token, so an administrator can never
-- send the members' tokens to a host of their choosing. The database does
-- it, so raw SQL cannot change the address and keep them.
-- +goose StatementBegin
CREATE FUNCTION exciter_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM exciter_token;
    END IF;
    RETURN NEW;
END;
$$;

-- Where Exciter is, for any member: the base URL links requirements. The
-- sealed secret stays behind.
CREATE FUNCTION exciter_endpoint() RETURNS TABLE (base_url text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url FROM exciter_connection c WHERE perm_is_member(current_actor_id())
$$;

-- How many members stored a token, for administrators; null for anybody else.
CREATE FUNCTION exciter_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM exciter_token)
           END
$$;
-- +goose StatementEnd

CREATE TRIGGER exciter_connection_moved BEFORE UPDATE ON exciter_connection
    FOR EACH ROW EXECUTE FUNCTION exciter_connection_moved();

-- A token row's id is always a fresh default, and its token is never
-- rewritten in place: a new token is a new row, so its cache is new too.
REVOKE ALL ON exciter_connection, exciter_token FROM stator_app;
GRANT SELECT, DELETE ON exciter_connection TO stator_app;
GRANT INSERT (base_url, webhook_secret, updated_by) ON exciter_connection TO stator_app;
GRANT UPDATE (base_url, webhook_secret, updated_by) ON exciter_connection TO stator_app;
GRANT SELECT, DELETE ON exciter_token TO stator_app;
GRANT INSERT (user_id, token, exciter_user_id, exciter_user_name, exciter_user_email, status, checked_at)
    ON exciter_token TO stator_app;
GRANT UPDATE (exciter_user_id, exciter_user_name, exciter_user_email, status, checked_at) ON exciter_token TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON exciter_connection, exciter_token TO stator_admin;
REVOKE ALL ON FUNCTION exciter_endpoint(), exciter_connected_count(), exciter_connection_moved() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION exciter_endpoint(), exciter_connected_count() TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS exciter_token;
DROP TABLE IF EXISTS exciter_connection;
DROP FUNCTION IF EXISTS exciter_connected_count();
DROP FUNCTION IF EXISTS exciter_endpoint();
DROP FUNCTION IF EXISTS exciter_connection_moved();
