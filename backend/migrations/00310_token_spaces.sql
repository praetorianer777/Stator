-- +goose Up
-- Tokens limited to spaces (#111), as Armature limits a key to projects: a
-- token may name the spaces it reaches, and then reaches no other, whatever
-- its owner may do there. Unlike Armature the limit is the database's too:
-- the transaction carries the token's spaces in app.token_spaces, and the
-- permission functions every policy calls read it.

-- True once a token names its spaces. Kept apart from the rows below so a
-- token whose spaces are all deleted reaches nothing, rather than everything.
ALTER TABLE api_token ADD COLUMN spaces_only boolean NOT NULL DEFAULT false;

-- Space ids rather than keys: a renamed space stays in every token that named
-- it, and a deleted one drops out of them.
CREATE TABLE api_token_space (
    org_id   uuid NOT NULL,
    token_id uuid NOT NULL,
    space_id uuid NOT NULL,
    PRIMARY KEY (token_id, space_id),
    FOREIGN KEY (org_id, token_id) REFERENCES api_token (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE
);

CREATE INDEX api_token_space_space_idx ON api_token_space (org_id, space_id);

-- The spaces of the token the transaction acts with, or NULL for a session or
-- a token that reaches every space its owner does.
-- +goose StatementBegin
CREATE FUNCTION current_token_spaces() RETURNS uuid[]
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.token_spaces', true), '')::uuid[]
$$;

-- Whether the transaction's token lets the actor reach a space. The limit is
-- the caller's alone: a rule asked about somebody else, as the access
-- inspector and the notification workers ask, answers for that person.
CREATE FUNCTION perm_token_reaches(actor uuid, space uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id()
        OR current_token_spaces() IS NULL
        OR COALESCE(space = ANY (current_token_spaces()), false)
$$;

-- Whether the actor may act on the organization as a whole, which a token
-- limited to spaces may not: that is outside every space it names.
CREATE FUNCTION perm_token_whole(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id() OR current_token_spaces() IS NULL
$$;

-- The owner and admin roles as stored, which hold every permission in every
-- space a token reaches.
CREATE FUNCTION perm_role_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE org_id = current_org_id() AND user_id = actor AND org_role IN ('owner', 'admin'))
$$;

-- Administering the organization, which every organization-wide policy asks.
CREATE OR REPLACE FUNCTION perm_is_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_role_admin(actor) AND perm_token_whole(actor)
$$;

-- use stays with a limited token, since every space permission needs it.
CREATE OR REPLACE FUNCTION perm_global_holds(actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT (permission = 'use' OR perm_token_whole(actor))
       AND (perm_role_admin(actor)
            OR (permission <> 'administer' AND ARRAY['use', permission] <@ perm_global_grants(actor)))
$$;

CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_reaches(actor, space)
       AND (perm_role_admin(actor)
            OR (perm_global_holds(actor, 'use') AND EXISTS (
                SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
                WHERE held.p = permission OR held.p = 'administer' OR permission = 'view')))
$$;

-- A token is made with its spaces, in the transaction that makes it, and a
-- row added to an older token would widen it.
CREATE FUNCTION api_token_space_addable(token uuid, space uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.org_id = current_org_id() AND t.id = token AND t.spaces_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_space_holds(current_actor_id(), space, 'view')
$$;

-- What a token reaches is fixed when it is made; widening one is making another.
CREATE FUNCTION api_token_spaces_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.spaces_only IS DISTINCT FROM OLD.spaces_only THEN
        RAISE EXCEPTION 'the spaces a token reaches are fixed when it is made; make a new token instead'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'api_token_spaces_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER api_token_spaces_fixed BEFORE UPDATE ON api_token
    FOR EACH ROW EXECUTE FUNCTION api_token_spaces_fixed();

ALTER TABLE api_token_space ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token_space FORCE  ROW LEVEL SECURITY;

CREATE POLICY api_token_space_tenant_isolation ON api_token_space
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- Authentication reads a token's spaces before any tenant is known.
CREATE POLICY api_token_space_admin_bypass ON api_token_space TO stator_admin USING (true) WITH CHECK (true);

-- No UPDATE: a row is added with its token and leaves with it or its space.
GRANT SELECT, INSERT, DELETE ON api_token_space TO stator_app, stator_admin;

CREATE POLICY api_token_space_added_with_token ON api_token_space AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (api_token_space_addable(token_id, space_id));

-- Tokens are the organization's business, not a space's, so a limited token
-- neither lists them nor makes or revokes one.
CREATE POLICY api_token_whole_reach ON api_token AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_token_whole(current_actor_id())) WITH CHECK (perm_token_whole(current_actor_id()));
CREATE POLICY api_token_space_whole_reach ON api_token_space AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_token_whole(current_actor_id())) WITH CHECK (perm_token_whole(current_actor_id()));

REVOKE EXECUTE ON FUNCTION api_token_space_addable(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION api_token_space_addable(uuid, uuid) TO stator_app, stator_admin;

-- +goose Down
DROP POLICY IF EXISTS api_token_whole_reach ON api_token;
DROP TRIGGER IF EXISTS api_token_spaces_fixed ON api_token;
DROP FUNCTION IF EXISTS api_token_spaces_fixed();
DROP TABLE IF EXISTS api_token_space;
DROP FUNCTION IF EXISTS api_token_space_addable(uuid, uuid);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_is_admin(actor)
        OR (perm_global_holds(actor, 'use') AND EXISTS (
            SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
            WHERE held.p = permission OR held.p = 'administer' OR permission = 'view'))
$$;

CREATE OR REPLACE FUNCTION perm_global_holds(actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_is_admin(actor)
        OR (permission <> 'administer' AND ARRAY['use', permission] <@ perm_global_grants(actor))
$$;

CREATE OR REPLACE FUNCTION perm_is_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE org_id = current_org_id() AND user_id = actor AND org_role IN ('owner', 'admin'))
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS perm_role_admin(uuid);
DROP FUNCTION IF EXISTS perm_token_whole(uuid);
DROP FUNCTION IF EXISTS perm_token_reaches(uuid, uuid);
DROP FUNCTION IF EXISTS current_token_spaces();
ALTER TABLE api_token DROP COLUMN IF EXISTS spaces_only;
