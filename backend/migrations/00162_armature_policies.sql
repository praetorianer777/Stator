-- +goose Up
-- Who may touch the Armature connection and the tokens (#27). Administrators
-- keep the connection; each member keeps their own token and nobody reads
-- anybody else's. What members need of the connection, and what
-- administrators need of the tokens, they read through the functions below.

-- A new address or Armature organization forgets every stored token, so an
-- administrator can never send the members' tokens to a host of their
-- choosing, and the organization id learned from the old one goes with them.
-- The database does it, so raw SQL cannot change the address and keep them.
-- +goose StatementBegin
CREATE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token WHERE org_id = OLD.org_id;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER armature_connection_moved BEFORE UPDATE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION armature_connection_moved();

-- Where the organization's Armature is, for any member: the base URL links
-- issues and the slug is what a token must belong to. The sealed secret stays
-- behind.
-- +goose StatementBegin
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, org_slug text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.org_slug, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id() AND perm_is_member(current_actor_id())
$$;

-- How many members stored a token, for administrators; null for anybody else.
CREATE FUNCTION armature_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM armature_token WHERE org_id = current_org_id())
           END
$$;
-- +goose StatementEnd

CREATE POLICY armature_connection_admins ON armature_connection AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY armature_token_own ON armature_token AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));

-- The learned organization id is the service's to write as the admin role.
-- A token row's id is always a fresh default, and its token is never
-- rewritten in place: a new token is a new row, so its cache is new too.
REVOKE ALL ON armature_connection, armature_token FROM stator_app;
GRANT SELECT, DELETE ON armature_connection TO stator_app;
GRANT INSERT (org_id, base_url, org_slug, webhook_secret, updated_by) ON armature_connection TO stator_app;
GRANT UPDATE (base_url, org_slug, webhook_secret, updated_by) ON armature_connection TO stator_app;
GRANT SELECT, DELETE ON armature_token TO stator_app;
GRANT INSERT (org_id, user_id, token, armature_user_id, armature_user_name, armature_user_email, status, checked_at)
    ON armature_token TO stator_app;
GRANT UPDATE (armature_user_id, armature_user_name, armature_user_email, status, checked_at) ON armature_token TO stator_app;
GRANT EXECUTE ON FUNCTION armature_endpoint(), armature_connected_count() TO stator_app;

-- +goose Down
DROP POLICY IF EXISTS armature_token_own ON armature_token;
DROP POLICY IF EXISTS armature_connection_admins ON armature_connection;
DROP FUNCTION IF EXISTS armature_connected_count();
DROP FUNCTION IF EXISTS armature_endpoint();
DROP TRIGGER IF EXISTS armature_connection_moved ON armature_connection;
DROP FUNCTION IF EXISTS armature_connection_moved();
