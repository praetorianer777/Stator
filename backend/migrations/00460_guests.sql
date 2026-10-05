-- +goose Up
-- Guests (#69): people from outside an organization, let into exactly one of
-- its spaces by an administrator. A guest is a member with the role guest and
-- the space they were invited to; everything else here holds them to it, so
-- a query that forgets them reaches no other space, nobody outside their
-- space and nothing of the organization as a whole.

ALTER TABLE org_member DROP CONSTRAINT org_member_org_role_check;
ALTER TABLE org_member ADD CONSTRAINT org_member_org_role_check
    CHECK (org_role IN ('owner', 'admin', 'member', 'guest'));

-- A guest goes with their space: deleting it takes their membership along.
ALTER TABLE org_member ADD COLUMN guest_space_id uuid;
ALTER TABLE org_member ADD CONSTRAINT org_member_guest_space_fkey
    FOREIGN KEY (org_id, guest_space_id) REFERENCES space (org_id, id) ON DELETE CASCADE;
ALTER TABLE org_member ADD CONSTRAINT org_member_guest_has_space
    CHECK ((org_role = 'guest') = (guest_space_id IS NOT NULL));
-- The provider's groups never decide a guest's role, as they never decide an owner's.
ALTER TABLE org_member ADD CONSTRAINT org_member_guest_is_manual
    CHECK (org_role <> 'guest' OR role_source = 'manual');

CREATE INDEX org_member_guest_space_idx ON org_member (org_id, guest_space_id) WHERE guest_space_id IS NOT NULL;

-- +goose StatementBegin
-- The one space a guest reaches, NULL for everybody else.
CREATE FUNCTION perm_guest_space(actor uuid) RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT guest_space_id FROM org_member WHERE org_id = current_org_id() AND user_id = actor
$$;

-- The people of a space, as a guest of it sees them: whoever a grant of the
-- space names, directly or through a group, and whoever published, commented
-- or owns a page there.
CREATE FUNCTION perm_space_person(space uuid, person uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.org_id = current_org_id() AND gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.org_id = p.org_id AND c.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.org_id = p.org_id AND o.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND o.user_id = person)
$$;

-- Whether the actor may see a person at all: everybody may, but a guest sees
-- only themselves and the people of their space.
CREATE FUNCTION perm_person_seen(actor uuid, person uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN gs IS NULL THEN true ELSE person = actor OR perm_space_person(gs, person) END
    FROM (SELECT perm_guest_space(actor) AS gs) AS g
$$;

-- A guest is held as a token limited to their space holds its caller, and
-- whoever asks about them: the access inspector and the workers too.
CREATE OR REPLACE FUNCTION perm_token_reaches(actor uuid, space uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT (actor IS DISTINCT FROM current_actor_id()
            OR current_token_spaces() IS NULL
            OR COALESCE(space = ANY (current_token_spaces()), false))
       AND COALESCE(perm_guest_space(actor) = space, true)
$$;

CREATE OR REPLACE FUNCTION perm_token_whole(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT (actor IS DISTINCT FROM current_actor_id() OR current_token_spaces() IS NULL)
       AND perm_guest_space(actor) IS NULL
$$;

-- A guest holds what grants naming them say, never what everyone holds.
CREATE OR REPLACE FUNCTION perm_space_grants(actor uuid, space uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g, (SELECT perm_guest_space(actor) AS gs) AS guest
    WHERE g.org_id = current_org_id() AND g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (guest.gs IS NULL OR (g.subject_type = 'user' AND g.space_id = guest.gs))
$$;

-- A guest is granted in their own space only, and never administers it:
-- administering would show them the space's grants, which name people and
-- groups from all over the organization.
CREATE FUNCTION space_grant_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gs uuid;
BEGIN
    IF NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
    IF gs IS NULL THEN
        RETURN NEW;
    END IF;
    IF gs <> NEW.space_id THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to. Invite somebody else, or make them a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_one_space';
    END IF;
    IF NEW.permission = 'administer' THEN
        RAISE EXCEPTION 'A guest cannot administer a space. Give them view, add pages, add comments or delete instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_not_administrator';
    END IF;
    RETURN NEW;
END;
$$;

-- Groups are the organization's; a group naming a guest would carry them
-- into every space the group is granted.
CREATE FUNCTION group_member_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot join a group. Grant them their space by name instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_not_guest';
    END IF;
    RETURN NEW;
END;
$$;

-- Being a guest, and of which space, is settled by the invitation: a change
-- either way would carry grants from one standing into the other.
CREATE FUNCTION org_member_guest_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (OLD.org_role = 'guest' OR NEW.org_role = 'guest')
       AND (NEW.org_role, NEW.guest_space_id) IS DISTINCT FROM (OLD.org_role, OLD.guest_space_id) THEN
        RAISE EXCEPTION 'A guest stays a guest of their one space. Remove them, then invite or let them in again.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'org_member_guest_fixed';
    END IF;
    RETURN NEW;
END;
$$;

-- A request names only people its author may see, so a guest cannot reach
-- somebody outside their space by writing an id into a page.
CREATE OR REPLACE FUNCTION outbox_mentions_members(payload jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT (payload ? 'mentioned')
        OR (jsonb_typeof(payload -> 'mentioned') = 'array'
            AND NOT EXISTS (
                SELECT 1 FROM jsonb_array_elements(payload -> 'mentioned') AS m (id)
                WHERE CASE WHEN jsonb_typeof(m.id) = 'string'
                            AND m.id #>> '{}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                           THEN NOT (perm_is_member((m.id #>> '{}')::uuid)
                                     AND perm_person_seen(current_actor_id(), (m.id #>> '{}')::uuid))
                           ELSE true END))
$$;
-- +goose StatementEnd

CREATE TRIGGER space_grant_guest_check BEFORE INSERT OR UPDATE ON space_grant
    FOR EACH ROW EXECUTE FUNCTION space_grant_guest_guard();
CREATE TRIGGER group_member_guest_check BEFORE INSERT OR UPDATE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_guest_guard();
CREATE TRIGGER org_member_guest_fixed BEFORE UPDATE OF org_role, guest_space_id ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_guest_fixed();

-- What a guest reads of people and groups. The guest's own space is computed
-- once per statement, as an InitPlan, so nobody else pays for it per row.
CREATE POLICY app_user_guest_sees ON app_user AS RESTRICTIVE FOR SELECT TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL
           OR id = current_actor_id()
           OR perm_space_person((SELECT perm_guest_space(current_actor_id())), id));
CREATE POLICY org_member_guest_sees ON org_member AS RESTRICTIVE FOR SELECT TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL
           OR user_id = current_actor_id()
           OR perm_space_person((SELECT perm_guest_space(current_actor_id())), user_id));
CREATE POLICY groups_not_guests ON groups AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL)
    WITH CHECK ((SELECT perm_guest_space(current_actor_id())) IS NULL);
CREATE POLICY group_member_not_guests ON group_member AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL)
    WITH CHECK ((SELECT perm_guest_space(current_actor_id())) IS NULL);

-- Who waits to be let in, and how the organization signs people in, are its
-- administrators' business; the service asks for administrators already.
CREATE POLICY org_join_request_not_guests ON org_join_request AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL)
    WITH CHECK ((SELECT perm_guest_space(current_actor_id())) IS NULL);
CREATE POLICY oidc_provider_not_guests ON oidc_provider AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL)
    WITH CHECK ((SELECT perm_guest_space(current_actor_id())) IS NULL);
CREATE POLICY oidc_group_role_not_guests ON oidc_group_role AS RESTRICTIVE FOR ALL TO stator_app
    USING ((SELECT perm_guest_space(current_actor_id())) IS NULL)
    WITH CHECK ((SELECT perm_guest_space(current_actor_id())) IS NULL);

-- Only an organization administrator makes a guest or takes one out; the
-- app role's other membership writes are as they were.
CREATE POLICY org_member_guests_by_admins_add ON org_member AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (org_role <> 'guest' OR perm_is_admin(current_actor_id()));
CREATE POLICY org_member_guests_by_admins_remove ON org_member AS RESTRICTIVE FOR DELETE TO stator_app
    USING (org_role <> 'guest' OR perm_is_admin(current_actor_id()));

REVOKE EXECUTE ON FUNCTION perm_guest_space(uuid), perm_space_person(uuid, uuid), perm_person_seen(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION perm_guest_space(uuid), perm_space_person(uuid, uuid), perm_person_seen(uuid, uuid) TO stator_app, stator_admin;

-- +goose Down
DROP POLICY IF EXISTS org_member_guests_by_admins_remove ON org_member;
DROP POLICY IF EXISTS org_member_guests_by_admins_add ON org_member;
DROP POLICY IF EXISTS oidc_group_role_not_guests ON oidc_group_role;
DROP POLICY IF EXISTS oidc_provider_not_guests ON oidc_provider;
DROP POLICY IF EXISTS org_join_request_not_guests ON org_join_request;
DROP POLICY IF EXISTS group_member_not_guests ON group_member;
DROP POLICY IF EXISTS groups_not_guests ON groups;
DROP POLICY IF EXISTS org_member_guest_sees ON org_member;
DROP POLICY IF EXISTS app_user_guest_sees ON app_user;
DROP TRIGGER IF EXISTS org_member_guest_fixed ON org_member;
DROP TRIGGER IF EXISTS group_member_guest_check ON group_member;
DROP TRIGGER IF EXISTS space_grant_guest_check ON space_grant;
DELETE FROM org_member WHERE org_role = 'guest';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION outbox_mentions_members(payload jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT (payload ? 'mentioned')
        OR (jsonb_typeof(payload -> 'mentioned') = 'array'
            AND NOT EXISTS (
                SELECT 1 FROM jsonb_array_elements(payload -> 'mentioned') AS m (id)
                WHERE CASE WHEN jsonb_typeof(m.id) = 'string'
                            AND m.id #>> '{}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                           THEN NOT perm_is_member((m.id #>> '{}')::uuid)
                           ELSE true END))
$$;

CREATE OR REPLACE FUNCTION perm_space_grants(actor uuid, space uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g
    WHERE g.org_id = current_org_id() AND g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$$;

CREATE OR REPLACE FUNCTION perm_token_whole(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id() OR current_token_spaces() IS NULL
$$;

CREATE OR REPLACE FUNCTION perm_token_reaches(actor uuid, space uuid) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT actor IS DISTINCT FROM current_actor_id()
        OR current_token_spaces() IS NULL
        OR COALESCE(space = ANY (current_token_spaces()), false)
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS org_member_guest_fixed();
DROP FUNCTION IF EXISTS group_member_guest_guard();
DROP FUNCTION IF EXISTS space_grant_guest_guard();
DROP FUNCTION IF EXISTS perm_person_seen(uuid, uuid);
DROP FUNCTION IF EXISTS perm_space_person(uuid, uuid);
DROP FUNCTION IF EXISTS perm_guest_space(uuid);
DROP INDEX IF EXISTS org_member_guest_space_idx;
ALTER TABLE org_member DROP CONSTRAINT IF EXISTS org_member_guest_is_manual;
ALTER TABLE org_member DROP CONSTRAINT IF EXISTS org_member_guest_has_space;
ALTER TABLE org_member DROP CONSTRAINT IF EXISTS org_member_guest_space_fkey;
ALTER TABLE org_member DROP COLUMN IF EXISTS guest_space_id;
ALTER TABLE org_member DROP CONSTRAINT org_member_org_role_check;
ALTER TABLE org_member ADD CONSTRAINT org_member_org_role_check
    CHECK (org_role IN ('owner', 'admin', 'member'));
