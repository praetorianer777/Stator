-- +goose Up
-- Access through roles, a matrix, groups and grants, as Armature has it (#585).
--
-- A role is a named list of permissions; the built-in ones cannot be deleted,
-- and administrators add their own. A grant is a role, a scope (the whole
-- installation or one space) and a subject (a person or a group). What
-- somebody may do in a space is the union of the grants that reach them. A
-- built-in group, Everyone, holds every member, and stands where the subject
-- "everyone" stood. Somebody's standing, member or guest, says only whether
-- they are in; administering the installation is a grant like any other.
--
-- Every grid entry and group role of before moves to the grant of the role it
-- amounts to, so nobody's access changes. The policies keep calling the same
-- functions, which now read the grants.

-- Not forced meanwhile, so that an owner that is no superuser reaches every
-- row the backfill reads and writes; see 00070 (#423).
ALTER TABLE member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
ALTER TABLE groups NO FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE space NO FORCE ROW LEVEL SECURITY;
ALTER TABLE space_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE global_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_group_role NO FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_provider NO FORCE ROW LEVEL SECURITY;

CREATE TABLE role (
    key                    text PRIMARY KEY,
    name                   text NOT NULL,
    description            text NOT NULL DEFAULT '',
    -- A role that answers for the whole installation only, never one space.
    installation_wide_only boolean NOT NULL DEFAULT false,
    builtin                boolean NOT NULL DEFAULT false,
    permissions            text[] NOT NULL DEFAULT '{}',
    sort_order             integer NOT NULL DEFAULT 100,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT role_key_shape CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT role_name_not_blank CHECK (btrim(name) <> ''),
    -- The permissions the code checks. A row naming another would grant a
    -- word nothing asks for.
    CONSTRAINT role_known_permissions CHECK (
        permissions <@ ARRAY['view', 'addComments', 'addPages', 'delete', 'administer',
                             'use', 'createSpace', 'administerSite'])
);

CREATE UNIQUE INDEX role_name_idx ON role (lower(name));

CREATE TRIGGER role_set_updated_at BEFORE UPDATE ON role
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO role (key, name, description, installation_wide_only, builtin, permissions, sort_order) VALUES
    ('administrator', 'Administrator',
     'Administers the installation: its people, roles, groups and settings, and every space in it.',
     true, true, ARRAY['view', 'addComments', 'addPages', 'delete', 'administer', 'use', 'createSpace', 'administerSite'], 10),
    ('space_administrator', 'Space administrator',
     'Administers a space: its details, permissions, trash and archive, and works in it like an editor.',
     false, true, ARRAY['view', 'addComments', 'addPages', 'delete', 'administer'], 20),
    ('editor', 'Editor',
     'Adds, edits and moves pages, comments, and moves pages to the trash and back.',
     false, true, ARRAY['view', 'addComments', 'addPages', 'delete'], 30),
    ('commenter', 'Commenter',
     'Reads a space and comments on its pages.',
     false, true, ARRAY['view', 'addComments'], 40),
    ('viewer', 'Viewer',
     'Reads a space and changes nothing in it.',
     false, true, ARRAY['view'], 50),
    -- Not built in: what the installation grants as a whole beside
    -- administering it was a grid entry before, and the two carry it on.
    ('member', 'Member',
     'Uses Stator: signs in and reaches what the grants on spaces open.',
     true, false, ARRAY['use'], 60),
    ('space_creator', 'Space creator',
     'Creates spaces, together with a role that lets them use Stator.',
     true, false, ARRAY['createSpace'], 70);

-- What a built-in role is stays: it cannot go and its key cannot change, the
-- administrator keeps administering the installation, or an edit could lock
-- every administrator out of the page that undoes it, and no role a guest
-- holds comes to administer a space.
-- +goose StatementBegin
CREATE FUNCTION role_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin THEN
            RAISE EXCEPTION 'A built-in role cannot be deleted. Take its grants away instead.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_builtin_stays';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin) THEN
        RAISE EXCEPTION 'A role keeps its key and whether it is built in.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_key_fixed';
    END IF;
    IF NEW.key = 'administrator' AND NOT ('administerSite' = ANY (NEW.permissions)) THEN
        RAISE EXCEPTION 'The administrator keeps administering Stator, or nobody could undo this.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_administrator_administers';
    END IF;
    IF TG_OP = 'UPDATE' AND 'administer' = ANY (NEW.permissions) AND NOT ('administer' = ANY (OLD.permissions))
       AND EXISTS (SELECT 1 FROM role_assignment a JOIN member m ON m.user_id = a.user_id
                   WHERE a.role = NEW.key AND m.role = 'guest') THEN
        RAISE EXCEPTION 'A guest holds this role, and a guest never administers a space. Take the role from the guests first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_not_administrator';
    END IF;
    IF TG_OP = 'UPDATE' AND 'administerSite' = ANY (NEW.permissions) AND NOT ('administerSite' = ANY (OLD.permissions))
       AND EXISTS (SELECT 1 FROM role_assignment a JOIN groups g ON g.id = a.group_id
                   WHERE a.role = NEW.key AND g.builtin = 'everyone') THEN
        RAISE EXCEPTION 'Everyone holds this role, so it cannot administer Stator. Take the role from Everyone first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_check BEFORE INSERT OR UPDATE OR DELETE ON role
    FOR EACH ROW EXECUTE FUNCTION role_guard();

-- Everyone: a group the installation keeps itself. A group somebody made
-- under the name keeps its members and grants under another.
ALTER TABLE groups ADD COLUMN builtin text
    CONSTRAINT groups_builtin_known CHECK (builtin IN ('everyone'));
CREATE UNIQUE INDEX groups_builtin_idx ON groups (builtin) WHERE builtin IS NOT NULL;

UPDATE groups SET name = name || ' (made by hand)' WHERE lower(name) = 'everyone';

-- A fixed id, so every copy of an installation, and every upgrade of one,
-- names Everyone alike.
INSERT INTO groups (id, name, description, builtin)
VALUES ('00000000-0000-7000-8000-00000000e0e0', 'Everyone', 'Every member of the installation, not guests.', 'everyone');

INSERT INTO group_member (group_id, user_id)
SELECT g.id, m.user_id FROM groups g, member m
WHERE g.builtin = 'everyone' AND m.role <> 'guest';

-- +goose StatementBegin
CREATE FUNCTION member_joins_everyone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.role <> 'guest' THEN
            INSERT INTO group_member (group_id, user_id)
            SELECT id, NEW.user_id FROM groups WHERE builtin = 'everyone'
            ON CONFLICT DO NOTHING;
        END IF;
        RETURN NULL;
    END IF;
    DELETE FROM group_member m USING groups g
    WHERE m.group_id = g.id AND g.builtin = 'everyone' AND m.user_id = OLD.user_id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER member_joins_everyone AFTER INSERT OR DELETE ON member
    FOR EACH ROW EXECUTE FUNCTION member_joins_everyone();

-- A member still here stays in Everyone, whoever asks.
-- +goose StatementBegin
CREATE FUNCTION group_member_keeps_everyone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM groups WHERE id = OLD.group_id AND builtin = 'everyone')
       AND EXISTS (SELECT 1 FROM member WHERE user_id = OLD.user_id AND role <> 'guest') THEN
        RAISE EXCEPTION 'Everyone holds every member, so nobody is taken out of it by hand.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_keeps_everyone';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER group_member_keeps_everyone BEFORE DELETE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_keeps_everyone();

-- +goose StatementBegin
CREATE FUNCTION groups_builtin_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin IS NOT NULL THEN
            RAISE EXCEPTION 'The group % is built in and cannot be deleted.', OLD.name
                USING ERRCODE = 'check_violation', CONSTRAINT = 'groups_builtin_fixed';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.builtin IS DISTINCT FROM OLD.builtin
       OR (OLD.builtin IS NOT NULL AND (NEW.name, NEW.description, NEW.source, NEW.external_ref)
           IS DISTINCT FROM (OLD.name, OLD.description, OLD.source, OLD.external_ref)) THEN
        RAISE EXCEPTION 'The group % is built in and cannot be changed.', OLD.name
            USING ERRCODE = 'check_violation', CONSTRAINT = 'groups_builtin_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER groups_builtin_guard BEFORE UPDATE OR DELETE ON groups
    FOR EACH ROW EXECUTE FUNCTION groups_builtin_guard();

CREATE TABLE role_assignment (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    role       text NOT NULL REFERENCES role(key) ON DELETE CASCADE,
    -- The space the role answers in; null for the whole installation.
    space_id   uuid REFERENCES space(id) ON DELETE CASCADE,
    user_id    uuid REFERENCES member(user_id) ON DELETE CASCADE,
    group_id   uuid REFERENCES groups(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT role_assignment_one_subject CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

-- The same grant twice is the same grant, the installation-wide ones too.
CREATE UNIQUE INDEX role_assignment_unique_idx
    ON role_assignment (role, space_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX role_assignment_space_idx ON role_assignment (space_id) WHERE space_id IS NOT NULL;
CREATE INDEX role_assignment_installation_idx ON role_assignment (role) WHERE space_id IS NULL;
CREATE INDEX role_assignment_user_idx ON role_assignment (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX role_assignment_group_idx ON role_assignment (group_id) WHERE group_id IS NOT NULL;

-- A guest belongs to their one space and never administers it, Everyone
-- never administers the installation, and a role for the whole installation
-- is not granted over one space.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gs uuid;
    perms text[];
    whole boolean;
BEGIN
    SELECT permissions, installation_wide_only INTO perms, whole FROM role WHERE key = NEW.role;
    IF NEW.space_id IS NOT NULL AND whole THEN
        RAISE EXCEPTION 'That role answers for the whole installation and cannot be granted in one space. Grant it without a space.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_installation_wide';
    END IF;
    IF NEW.user_id IS NOT NULL THEN
        SELECT guest_space_id INTO gs FROM member WHERE user_id = NEW.user_id;
        IF gs IS NOT NULL AND NEW.space_id IS DISTINCT FROM gs THEN
            RAISE EXCEPTION 'A guest belongs to the one space they were invited to. Invite somebody else, or make them a member first.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_one_space';
        END IF;
        IF gs IS NOT NULL AND 'administer' = ANY (perms) THEN
            RAISE EXCEPTION 'A guest cannot administer a space. Give them viewer, commenter or editor instead.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_not_administrator';
        END IF;
    END IF;
    IF NEW.group_id IS NOT NULL AND 'administerSite' = ANY (perms)
       AND EXISTS (SELECT 1 FROM groups WHERE id = NEW.group_id AND builtin = 'everyone') THEN
        RAISE EXCEPTION 'Everyone cannot administer Stator, or every member would. Grant the role to people or a group of their own.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_assignment_check BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();

-- What a role holds in a space, as the rules read it: administer holds every
-- space permission, and any of them holds view.
-- +goose StatementBegin
CREATE FUNCTION perm_space_closure(perms text[]) RETURNS text[]
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT ARRAY(
        SELECT w FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete', 'administer']) WITH ORDINALITY AS t (w, n)
        WHERE w = ANY (perms) OR 'administer' = ANY (perms)
           OR (w = 'view' AND perms && ARRAY['addPages', 'addComments', 'delete'])
        ORDER BY n)
$$;
-- +goose StatementEnd

-- The role that holds exactly these space permissions: a built-in one where
-- one does, else one made before, else a new one named for what it holds.
-- Used where a set of permissions comes in, as a grid of before did.
-- +goose StatementBegin
CREATE FUNCTION role_for_space_permissions(wanted text[]) RETURNS text
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    held text[] := perm_space_closure(wanted);
    found text;
    words text[];
    label text;
    candidate text;
    n integer := 1;
BEGIN
    IF cardinality(held) = 0 THEN
        RETURN NULL;
    END IF;
    SELECT r.key INTO found FROM role r
    WHERE NOT r.installation_wide_only
      AND perm_space_closure(r.permissions) = held
      AND r.permissions <@ ARRAY['view', 'addPages', 'addComments', 'delete', 'administer']
    ORDER BY r.builtin DESC, r.sort_order, r.key
    LIMIT 1;
    IF found IS NOT NULL THEN
        RETURN found;
    END IF;
    words := ARRAY(
        SELECT CASE w WHEN 'view' THEN 'view' WHEN 'addPages' THEN 'add pages' WHEN 'addComments' THEN 'comment'
                      WHEN 'delete' THEN 'delete' ELSE w END
        FROM unnest(held) AS w);
    label := CASE cardinality(words)
        WHEN 1 THEN words[1]
        ELSE array_to_string(words[1:cardinality(words) - 1], ', ') || ' and ' || words[cardinality(words)] END;
    label := upper(left(label, 1)) || substr(label, 2);
    found := left(regexp_replace(lower(label), '[^a-z0-9]+', '_', 'g'), 36);
    candidate := label;
    -- Another transaction may make the same role at once; whichever comes
    -- second takes the first's, or the next free key and name.
    LOOP
        INSERT INTO role (key, name, description, permissions)
        VALUES (found, candidate, 'Holds what a grant of before held that no built-in role matches.', held)
        ON CONFLICT DO NOTHING;
        IF EXISTS (SELECT 1 FROM role r
                   WHERE r.key = found AND NOT r.installation_wide_only
                     AND perm_space_closure(r.permissions) = held
                     AND r.permissions <@ ARRAY['view', 'addPages', 'addComments', 'delete', 'administer']) THEN
            RETURN found;
        END IF;
        n := n + 1;
        found := left(regexp_replace(lower(label), '[^a-z0-9]+', '_', 'g'), 36) || '_' || n;
        candidate := label || ' (' || n || ')';
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- The grids of before. Each subject's permissions in a space become the role
-- that holds just those; "everyone" is the Everyone group now.
INSERT INTO role_assignment (role, space_id, user_id, group_id)
SELECT role_for_space_permissions(array_agg(g.permission)), g.space_id, g.user_id,
       CASE WHEN g.subject_type = 'everyone' THEN (SELECT id FROM groups WHERE builtin = 'everyone') ELSE g.group_id END
FROM space_grant g
WHERE g.subject_type <> 'anonymous'
GROUP BY g.space_id, g.subject_type, g.user_id, g.group_id
ON CONFLICT DO NOTHING;

-- What the installation granted as a whole: use to the members, creating
-- spaces to the space creators.
INSERT INTO role_assignment (role, user_id, group_id)
SELECT CASE g.permission WHEN 'use' THEN 'member' ELSE 'space_creator' END, g.user_id,
       CASE WHEN g.subject_type = 'everyone' THEN (SELECT id FROM groups WHERE builtin = 'everyone') ELSE g.group_id END
FROM global_grant g
ON CONFLICT DO NOTHING;

-- Reading a space without signing in is the space's own switch now.
ALTER TABLE space ADD COLUMN anonymous_view boolean NOT NULL DEFAULT false;
ALTER TABLE space ADD CONSTRAINT space_anonymous_team_space CHECK (NOT anonymous_view OR owner_id IS NULL);
UPDATE space s SET anonymous_view = true
WHERE EXISTS (SELECT 1 FROM space_grant g WHERE g.space_id = s.id AND g.subject_type = 'anonymous' AND g.permission = 'view');

-- A provider group mapped to a role becomes a group with the provider's name
-- in its claim, granted the role it amounts to: admin the administrator,
-- member the member role, which lets its people in as it did.
INSERT INTO groups (name, source, external_ref)
SELECT CASE WHEN EXISTS (SELECT 1 FROM groups o WHERE lower(o.name) = lower(r.group_ref))
            THEN r.group_ref || ' (provider)' ELSE r.group_ref END,
       'oidc', r.group_ref
FROM oidc_group_role r
WHERE NOT EXISTS (SELECT 1 FROM groups g WHERE g.external_ref = r.group_ref);

INSERT INTO role_assignment (role, group_id)
SELECT CASE r.role WHEN 'admin' THEN 'administrator' ELSE 'member' END, g.id
FROM oidc_group_role r JOIN groups g ON g.external_ref = r.group_ref
ON CONFLICT DO NOTHING;

-- Owners and administrators keep administering: by name, unless a group
-- they are in already does so for them, as one that followed the provider's
-- groups did.
INSERT INTO role_assignment (role, user_id)
SELECT 'administrator', m.user_id FROM member m
WHERE m.role IN ('owner', 'admin')
  AND (m.role_source = 'manual' OR NOT EXISTS (
      SELECT 1 FROM role_assignment a JOIN group_member gm ON gm.group_id = a.group_id
      WHERE a.role = 'administrator' AND a.space_id IS NULL AND gm.user_id = m.user_id))
ON CONFLICT DO NOTHING;

-- Standing says only whether somebody is in.
DROP TRIGGER member_owner_stays ON member;
DROP FUNCTION member_owner_stays();
ALTER TABLE member DROP CONSTRAINT member_owner_is_manual;
ALTER TABLE member DROP CONSTRAINT member_guest_is_manual;
ALTER TABLE member DROP CONSTRAINT member_role_check;
UPDATE member SET role = 'member' WHERE role IN ('owner', 'admin');
ALTER TABLE member DROP COLUMN role_source;
ALTER TABLE member ADD CONSTRAINT member_role_check CHECK (role IN ('member', 'guest'));

DROP TABLE space_grant;
DROP FUNCTION space_grant_anonymous_guard();
DROP FUNCTION space_grant_guest_guard();
DROP FUNCTION space_grant_keeps_administrator();
DROP TABLE global_grant;
DROP TABLE oidc_group_role;
DROP FUNCTION perm_global_grant_sources(uuid, text);

-- The rules, as the policies read them.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_global_grants(actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT p.word ORDER BY p.word), '{}')
    FROM role_assignment a
    JOIN role r ON r.key = a.role
    CROSS JOIN LATERAL unnest(r.permissions) AS p (word)
    WHERE a.space_id IS NULL
      AND perm_is_member(actor) AND perm_guest_space(actor) IS NULL
      AND (a.user_id = actor OR (a.group_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = actor)))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_role_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT 'administerSite' = ANY (perm_global_grants(actor))
$$;
-- +goose StatementEnd

-- A guest uses Stator in their one space: their invitation said so.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_global_holds(actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT (permission = 'use' OR perm_token_whole(actor))
       AND CASE
           WHEN perm_guest_space(actor) IS NOT NULL THEN permission = 'use'
           WHEN permission = 'administer' THEN perm_role_admin(actor)
           ELSE ARRAY['use', permission] <@ perm_global_grants(actor)
       END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_grants(actor uuid, space uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT p.word ORDER BY p.word), '{}')
    FROM role_assignment a
    JOIN role r ON r.key = a.role
    CROSS JOIN LATERAL unnest(r.permissions) AS p (word),
    (SELECT perm_guest_space(actor) AS gs) AS guest
    WHERE (a.space_id = space OR a.space_id IS NULL)
      AND p.word IN ('view', 'addPages', 'addComments', 'delete', 'administer')
      AND perm_is_member(actor)
      AND (a.user_id = actor OR (a.group_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = actor)))
      AND (guest.gs IS NULL OR (a.user_id = actor AND a.space_id = guest.gs))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN actor IS NULL
        THEN current_anonymous() AND permission = 'view' AND perm_space_anonymous(space)
        ELSE perm_token_reaches(actor, space)
             AND perm_global_holds(actor, 'use') AND EXISTS (
                 SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
                 WHERE held.p = permission OR held.p = 'administer' OR permission = 'view')
    END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_anonymous(space uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM site o WHERE o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.anonymous_view)
$$;
-- +goose StatementEnd

-- Whom a guest of the space sees: the people its grants name, not those of
-- Everyone, who are every member.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_person(space uuid, person uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM role_assignment a
                   WHERE a.space_id = space
                     AND (a.user_id = person OR (a.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm JOIN groups g ON g.id = gm.group_id
                         WHERE gm.group_id = a.group_id AND gm.user_id = person AND g.builtin IS NULL))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.page_id = p.id
                   WHERE p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.page_id = p.id
                   WHERE p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.page_id = p.id
                   WHERE p.space_id = space AND o.user_id = person)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_open_to_members(page_id uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id))
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN chain c ON c.id = r.page_id
                       WHERE r.kind = 'view')
       AND EXISTS (SELECT 1 FROM role_assignment a
                   JOIN groups g ON g.id = a.group_id AND g.builtin = 'everyone'
                   JOIN role r ON r.key = a.role
                   WHERE (a.space_id = (SELECT space_id FROM chain LIMIT 1) OR a.space_id IS NULL)
                     AND cardinality(perm_space_closure(r.permissions)) > 0)
$$;
-- +goose StatementEnd

-- The grants that reach a person in a space or across the installation, for
-- the access inspector; the person themselves, an administrator of the
-- installation, or one of the space reads them.
-- +goose StatementBegin
CREATE FUNCTION perm_grants_reaching(person uuid, space uuid)
    RETURNS TABLE (id uuid, role text, space_id uuid, user_id uuid, group_id uuid, created_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.id, a.role, a.space_id, a.user_id, a.group_id, a.created_at
    FROM role_assignment a
    WHERE (a.space_id = space OR a.space_id IS NULL)
      AND perm_is_member(person)
      AND (a.user_id = person OR (a.group_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = person)))
      AND (person = current_actor_id() OR perm_is_admin(current_actor_id())
           OR perm_space_holds(current_actor_id(), space, 'administer'))
$$;
-- +goose StatementEnd

-- A new installation lets its members use Stator.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION site_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO role_assignment (role, group_id)
    SELECT 'member', id FROM groups WHERE builtin = 'everyone'
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A new team space lets every member edit in it; whoever made it administers it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.owner_id IS NULL THEN
        INSERT INTO role_assignment (role, space_id, group_id)
        SELECT 'editor', NEW.id, id FROM groups WHERE builtin = 'everyone';
    END IF;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM member WHERE user_id = NEW.created_by) THEN
        INSERT INTO role_assignment (role, space_id, user_id, created_by)
        VALUES ('space_administrator', NEW.id, NEW.created_by, NEW.created_by);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A space keeps an administrator of its own (#82), as in 00490: taking the
-- last grant that administers it is refused, unless an administrator of the
-- installation does it, or the subject or the space went. Checked at commit.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_keeps_space_administrator() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.space_id IS NULL OR NOT EXISTS (SELECT 1 FROM space WHERE id = OLD.space_id) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM role r WHERE r.key = OLD.role AND NOT ('administer' = ANY (r.permissions))) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
               WHERE a.space_id = OLD.space_id AND 'administer' = ANY (r.permissions)) THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM role WHERE key = OLD.role)
       OR (OLD.user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM member WHERE user_id = OLD.user_id))
       OR (OLD.group_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM groups WHERE id = OLD.group_id)) THEN
        RETURN NULL;
    END IF;
    IF current_actor_id() IS NULL OR perm_is_admin(current_actor_id()) THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'A space keeps at least one administrator. Grant space administrator to another person or group before you take it from the last one.'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'space_keeps_administrator';
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER role_assignment_keeps_space_administrator
    AFTER DELETE OR UPDATE OF role, space_id ON role_assignment
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION role_assignment_keeps_space_administrator();

-- The installation keeps an active administrator: somebody switched on, a
-- member, who holds administering the installation through a grant of their
-- own or of a group. Whatever change would leave none is refused at commit.
-- +goose StatementBegin
CREATE FUNCTION perm_active_administrators() RETURNS bigint
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(DISTINCT m.user_id)
    FROM member m
    JOIN app_user u ON u.id = m.user_id AND u.is_active
    WHERE m.role <> 'guest'
      AND EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                  WHERE a.space_id IS NULL AND 'administerSite' = ANY (r.permissions)
                    AND (a.user_id = m.user_id OR EXISTS (
                        SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = m.user_id)))
$$;
-- +goose StatementEnd

-- Only a change that took administering from somebody is weighed: an
-- installation that has no administrator yet, as a fresh one or a test's,
-- still lets everybody else come and go.
-- +goose StatementBegin
CREATE FUNCTION installation_keeps_administrator() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    took boolean;
    who uuid;
BEGIN
    IF TG_TABLE_NAME = 'role_assignment' THEN
        took := NOT EXISTS (SELECT 1 FROM role r WHERE r.key = OLD.role AND NOT ('administerSite' = ANY (r.permissions)));
    ELSIF TG_TABLE_NAME = 'group_member' THEN
        took := EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                        WHERE a.group_id = OLD.group_id AND a.space_id IS NULL AND 'administerSite' = ANY (r.permissions));
    ELSIF TG_TABLE_NAME IN ('member', 'app_user') THEN
        who := CASE WHEN TG_TABLE_NAME = 'member' THEN (to_jsonb(OLD) ->> 'user_id')::uuid ELSE (to_jsonb(OLD) ->> 'id')::uuid END;
        took := EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                        WHERE a.space_id IS NULL AND 'administerSite' = ANY (r.permissions)
                          AND (a.user_id = who OR EXISTS (
                              SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = who)));
    ELSE
        took := true;
    END IF;
    IF NOT took OR perm_active_administrators() > 0 OR NOT EXISTS (SELECT 1 FROM member WHERE role <> 'guest') THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'Stator keeps at least one active administrator. Make somebody else an administrator first, then take it from this one.'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'installation_keeps_administrator';
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER DELETE OR UPDATE ON role_assignment
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (OLD.space_id IS NULL)
    EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER UPDATE OF permissions OR DELETE ON role
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN ('administerSite' = ANY (OLD.permissions))
    EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER DELETE ON group_member
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER DELETE ON member
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER UPDATE OF is_active ON app_user
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (OLD.is_active AND NOT NEW.is_active)
    EXECUTE FUNCTION installation_keeps_administrator();

-- Every member reads the roles; the installation's administrators change them.
ALTER TABLE role ENABLE ROW LEVEL SECURITY;
ALTER TABLE role FORCE ROW LEVEL SECURITY;
CREATE POLICY role_app ON role USING (true) WITH CHECK (true);
CREATE POLICY role_admin_bypass ON role TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY role_not_anonymous ON role AS RESTRICTIVE TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY role_members_read ON role AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_is_member(current_actor_id()));
CREATE POLICY role_admins_add ON role AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY role_admins_change ON role AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY role_admins_remove ON role AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_is_admin(current_actor_id()));

-- A grant across the installation is its administrators' to read and give; a
-- grant in a space, the space's administrators' too.
ALTER TABLE role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY role_assignment_app ON role_assignment USING (true) WITH CHECK (true);
CREATE POLICY role_assignment_admin_bypass ON role_assignment TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY role_assignment_not_anonymous ON role_assignment AS RESTRICTIVE TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY role_assignment_administrators ON role_assignment AS RESTRICTIVE TO stator_app
    USING (perm_is_admin(current_actor_id())
           OR (space_id IS NOT NULL AND perm_space_holds(current_actor_id(), space_id, 'administer')))
    WITH CHECK (perm_is_admin(current_actor_id())
           OR (space_id IS NOT NULL AND perm_space_holds(current_actor_id(), space_id, 'administer')));

GRANT SELECT, INSERT, UPDATE, DELETE ON role, role_assignment TO stator_app, stator_admin;

ALTER TABLE oidc_provider FORCE ROW LEVEL SECURITY;
ALTER TABLE space FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE ROW LEVEL SECURITY;
ALTER TABLE groups FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;
ALTER TABLE member FORCE ROW LEVEL SECURITY;

-- +goose Down
ALTER TABLE member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
ALTER TABLE groups NO FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE space NO FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_provider NO FORCE ROW LEVEL SECURITY;
ALTER TABLE role NO FORCE ROW LEVEL SECURITY;
ALTER TABLE role_assignment NO FORCE ROW LEVEL SECURITY;

DROP TRIGGER installation_keeps_administrator ON role_assignment;
DROP TRIGGER installation_keeps_administrator ON role;
DROP TRIGGER installation_keeps_administrator ON group_member;
DROP TRIGGER installation_keeps_administrator ON member;
DROP TRIGGER installation_keeps_administrator ON app_user;
DROP FUNCTION installation_keeps_administrator();
DROP FUNCTION perm_active_administrators();
DROP TRIGGER role_assignment_keeps_space_administrator ON role_assignment;
DROP FUNCTION role_assignment_keeps_space_administrator();
DROP TRIGGER group_member_keeps_everyone ON group_member;
DROP FUNCTION group_member_keeps_everyone();
DROP TRIGGER member_joins_everyone ON member;
DROP FUNCTION member_joins_everyone();
DROP TRIGGER groups_builtin_guard ON groups;
DROP FUNCTION groups_builtin_guard();

ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD COLUMN role_source text NOT NULL DEFAULT 'manual';
ALTER TABLE member ADD CONSTRAINT member_role_check CHECK (role IN ('owner', 'admin', 'member', 'guest'));
ALTER TABLE member ADD CONSTRAINT member_role_source_check CHECK (role_source IN ('manual', 'oidc'));
UPDATE member m SET role = 'admin',
       role_source = CASE WHEN EXISTS (
           SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
           WHERE a.space_id IS NULL AND a.user_id = m.user_id AND 'administerSite' = ANY (r.permissions))
           THEN 'manual' ELSE 'oidc' END
WHERE m.role = 'member' AND 'administerSite' = ANY (perm_global_grants(m.user_id));
UPDATE member SET role = 'owner', role_source = 'manual'
WHERE user_id = (SELECT user_id FROM member WHERE role = 'admin' ORDER BY role_source = 'oidc', created_at, user_id LIMIT 1);
ALTER TABLE member ADD CONSTRAINT member_owner_is_manual CHECK (role <> 'owner' OR role_source = 'manual');
ALTER TABLE member ADD CONSTRAINT member_guest_is_manual CHECK (role <> 'guest' OR role_source = 'manual');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_owner_stays()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.role = 'owner' AND NEW.role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of the site''s owner'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_anonymous_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.subject_type = 'anonymous'
       AND EXISTS (SELECT 1 FROM space WHERE id = NEW.space_id AND owner_id IS NOT NULL) THEN
        RAISE EXCEPTION 'A personal space cannot be read without signing in. Move the pages to a team space to publish them.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_anonymous_team_space';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_guest_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gs uuid;
BEGIN
    IF NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM member WHERE user_id = NEW.user_id;
    IF gs IS NULL THEN
        RETURN NEW;
    END IF;
    IF gs <> NEW.space_id THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to. Invite somebody else, or make them a member first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_one_space';
    END IF;
    IF NEW.permission = 'administer' THEN
        RAISE EXCEPTION 'A guest cannot administer a space. Give them view, add pages, add comments or delete instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_not_administrator';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_keeps_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.permission <> 'administer' THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM space WHERE id = OLD.space_id) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM space_grant
               WHERE space_id = OLD.space_id AND permission = 'administer') THEN
        RETURN NULL;
    END IF;
    IF (OLD.user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM member WHERE user_id = OLD.user_id))
       OR (OLD.group_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM groups WHERE id = OLD.group_id)) THEN
        RETURN NULL;
    END IF;
    IF current_actor_id() IS NULL OR perm_is_admin(current_actor_id()) THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'A space keeps at least one administrator. Give administer to another person or group before you take it from the last one.'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_keeps_administrator';
END;
$function$;
-- +goose StatementEnd

CREATE TRIGGER member_owner_stays BEFORE UPDATE OF role, role_source ON member
    FOR EACH ROW EXECUTE FUNCTION member_owner_stays();

CREATE TABLE global_grant (
    id uuid DEFAULT uuidv7() NOT NULL PRIMARY KEY,
    permission text NOT NULL,
    subject_type text NOT NULL,
    user_id uuid REFERENCES member(user_id) ON DELETE CASCADE,
    group_id uuid REFERENCES groups(id) ON DELETE CASCADE,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT global_grant_permission_check CHECK ((permission = ANY (ARRAY['use'::text, 'createSpace'::text]))),
    CONSTRAINT global_grant_subject CHECK ((((subject_type = 'user'::text) AND (user_id IS NOT NULL) AND (group_id IS NULL)) OR ((subject_type = 'group'::text) AND (group_id IS NOT NULL) AND (user_id IS NULL)) OR ((subject_type = 'everyone'::text) AND (user_id IS NULL) AND (group_id IS NULL)))),
    CONSTRAINT global_grant_subject_type_check CHECK ((subject_type = ANY (ARRAY['user'::text, 'group'::text, 'everyone'::text])))
);
CREATE UNIQUE INDEX global_grant_unique_idx ON global_grant USING btree (permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;

CREATE TABLE oidc_group_role (
    id uuid DEFAULT uuidv7() NOT NULL PRIMARY KEY,
    group_ref text NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider boolean DEFAULT true NOT NULL REFERENCES oidc_provider(singleton) ON DELETE CASCADE,
    CONSTRAINT oidc_group_role_provider_one CHECK (provider),
    CONSTRAINT oidc_group_role_ref_not_blank CHECK ((btrim(group_ref) <> ''::text)),
    CONSTRAINT oidc_group_role_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text])))
);
CREATE UNIQUE INDEX oidc_group_role_ref_idx ON oidc_group_role USING btree (group_ref);
CREATE TRIGGER oidc_group_role_set_updated_at BEFORE UPDATE ON oidc_group_role FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE space_grant (
    id uuid DEFAULT uuidv7() NOT NULL PRIMARY KEY,
    space_id uuid NOT NULL REFERENCES space(id) ON DELETE CASCADE,
    permission text NOT NULL,
    subject_type text NOT NULL,
    user_id uuid REFERENCES member(user_id) ON DELETE CASCADE,
    group_id uuid REFERENCES groups(id) ON DELETE CASCADE,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT space_grant_permission_check CHECK ((permission = ANY (ARRAY['view'::text, 'addPages'::text, 'addComments'::text, 'delete'::text, 'administer'::text]))),
    CONSTRAINT space_grant_subject CHECK ((((subject_type = 'user'::text) AND (user_id IS NOT NULL) AND (group_id IS NULL)) OR ((subject_type = 'group'::text) AND (group_id IS NOT NULL) AND (user_id IS NULL)) OR ((subject_type = 'everyone'::text) AND (user_id IS NULL) AND (group_id IS NULL)) OR ((subject_type = 'anonymous'::text) AND (user_id IS NULL) AND (group_id IS NULL) AND (permission = 'view'::text)))),
    CONSTRAINT space_grant_subject_type_check CHECK ((subject_type = ANY (ARRAY['user'::text, 'group'::text, 'everyone'::text, 'anonymous'::text])))
);
CREATE UNIQUE INDEX space_grant_unique_idx ON space_grant USING btree (space_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE TRIGGER space_grant_anonymous_check BEFORE INSERT OR UPDATE ON space_grant FOR EACH ROW EXECUTE FUNCTION space_grant_anonymous_guard();
CREATE TRIGGER space_grant_guest_check BEFORE INSERT OR UPDATE ON space_grant FOR EACH ROW EXECUTE FUNCTION space_grant_guest_guard();
CREATE CONSTRAINT TRIGGER space_grant_keeps_administrator AFTER DELETE OR UPDATE OF permission, space_id ON space_grant DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION space_grant_keeps_administrator();

INSERT INTO space_grant (space_id, permission, subject_type, user_id, group_id)
SELECT DISTINCT a.space_id, p.word,
       CASE WHEN g.builtin = 'everyone' THEN 'everyone' WHEN a.user_id IS NOT NULL THEN 'user' ELSE 'group' END,
       a.user_id, CASE WHEN g.builtin = 'everyone' THEN NULL ELSE a.group_id END
FROM role_assignment a
JOIN role r ON r.key = a.role
CROSS JOIN LATERAL unnest(r.permissions) AS p (word)
LEFT JOIN groups g ON g.id = a.group_id
WHERE a.space_id IS NOT NULL AND p.word IN ('view', 'addPages', 'addComments', 'delete', 'administer')
ON CONFLICT DO NOTHING;
INSERT INTO space_grant (space_id, permission, subject_type)
SELECT id, 'view', 'anonymous' FROM space WHERE anonymous_view;

INSERT INTO global_grant (permission, subject_type, user_id, group_id)
SELECT DISTINCT p.word,
       CASE WHEN g.builtin = 'everyone' THEN 'everyone' WHEN a.user_id IS NOT NULL THEN 'user' ELSE 'group' END,
       a.user_id, CASE WHEN g.builtin = 'everyone' THEN NULL ELSE a.group_id END
FROM role_assignment a
JOIN role r ON r.key = a.role
CROSS JOIN LATERAL unnest(r.permissions) AS p (word)
LEFT JOIN groups g ON g.id = a.group_id
WHERE a.space_id IS NULL AND p.word IN ('use', 'createSpace')
ON CONFLICT DO NOTHING;

INSERT INTO oidc_group_role (group_ref, role)
SELECT g.external_ref, CASE WHEN bool_or('administerSite' = ANY (r.permissions)) THEN 'admin' ELSE 'member' END
FROM role_assignment a
JOIN role r ON r.key = a.role
JOIN groups g ON g.id = a.group_id
WHERE a.space_id IS NULL AND g.external_ref IS NOT NULL AND EXISTS (SELECT 1 FROM oidc_provider)
GROUP BY g.external_ref
ON CONFLICT DO NOTHING;

ALTER TABLE global_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE global_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY global_grant_admin_bypass ON global_grant TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY global_grant_admins ON global_grant AS RESTRICTIVE TO stator_app USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY global_grant_app ON global_grant USING (true) WITH CHECK (true);
CREATE POLICY global_grant_not_anonymous ON global_grant AS RESTRICTIVE TO stator_app USING ((NOT current_anonymous())) WITH CHECK ((NOT current_anonymous()));
ALTER TABLE oidc_group_role ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_group_role FORCE ROW LEVEL SECURITY;
CREATE POLICY oidc_group_role_admin_bypass ON oidc_group_role TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY oidc_group_role_admins ON oidc_group_role AS RESTRICTIVE TO stator_app USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY oidc_group_role_app ON oidc_group_role USING (true) WITH CHECK (true);
CREATE POLICY oidc_group_role_not_anonymous ON oidc_group_role AS RESTRICTIVE TO stator_app USING ((NOT current_anonymous())) WITH CHECK ((NOT current_anonymous()));
CREATE POLICY oidc_group_role_not_guests ON oidc_group_role AS RESTRICTIVE TO stator_app USING ((( SELECT perm_guest_space(current_actor_id()) AS perm_guest_space) IS NULL)) WITH CHECK ((( SELECT perm_guest_space(current_actor_id()) AS perm_guest_space) IS NULL));
ALTER TABLE space_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE space_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY space_grant_admin_bypass ON space_grant TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY space_grant_administrators ON space_grant AS RESTRICTIVE TO stator_app USING (perm_space_holds(current_actor_id(), space_id, 'administer'::text)) WITH CHECK (perm_space_holds(current_actor_id(), space_id, 'administer'::text));
CREATE POLICY space_grant_app ON space_grant USING (true) WITH CHECK (true);
CREATE POLICY space_grant_not_anonymous ON space_grant AS RESTRICTIVE TO stator_app USING ((NOT current_anonymous())) WITH CHECK ((NOT current_anonymous()));
GRANT SELECT, INSERT, UPDATE, DELETE ON global_grant, oidc_group_role, space_grant TO stator_app, stator_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_open_to_members(page_id uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id))
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN chain c ON c.id = r.page_id
                       WHERE r.kind = 'view')
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = (SELECT space_id FROM chain LIMIT 1)
                     AND g.subject_type = 'everyone')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_global_grant_sources(actor uuid, permission text)
 RETURNS TABLE(subject_type text, user_id uuid, group_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT g.subject_type, g.user_id, g.group_id
    FROM global_grant g
    WHERE g.permission = perm_global_grant_sources.permission
      AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (actor = current_actor_id() OR perm_is_admin(current_actor_id()) OR EXISTS (
          SELECT 1 FROM space s
          WHERE perm_space_holds(current_actor_id(), s.id, 'administer')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_global_grants(actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM global_grant g
    WHERE perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_global_holds(actor uuid, permission text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT (permission = 'use' OR perm_token_whole(actor))
       AND (perm_role_admin(actor)
            OR (permission <> 'administer' AND ARRAY['use', permission] <@ perm_global_grants(actor)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_role_admin(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM member
                   WHERE user_id = actor AND role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_anonymous(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM site o WHERE o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_grants(actor uuid, space uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g, (SELECT perm_guest_space(actor) AS gs) AS guest
    WHERE g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (guest.gs IS NULL OR (g.subject_type = 'user' AND g.space_id = guest.gs))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN actor IS NULL
        THEN current_anonymous() AND permission = 'view' AND perm_space_anonymous(space)
        ELSE perm_token_reaches(actor, space)
             AND (perm_role_admin(actor)
                  OR (perm_global_holds(actor, 'use') AND EXISTS (
                      SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
                      WHERE held.p = permission OR held.p = 'administer' OR permission = 'view')))
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_person(space uuid, person uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.page_id = p.id
                   WHERE p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.page_id = p.id
                   WHERE p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.page_id = p.id
                   WHERE p.space_id = space AND o.user_id = person)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION site_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    INSERT INTO global_grant (permission, subject_type) VALUES ('use', 'everyone');
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.owner_id IS NULL THEN
        INSERT INTO space_grant (space_id, permission, subject_type)
        SELECT NEW.id, p, 'everyone' FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;
    END IF;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM member WHERE user_id = NEW.created_by) THEN
        INSERT INTO space_grant (space_id, permission, subject_type, user_id)
        VALUES (NEW.id, 'administer', 'user', NEW.created_by);
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

DROP FUNCTION perm_grants_reaching(uuid, uuid);
DROP TABLE role_assignment;
DROP FUNCTION role_assignment_guard();
DROP TABLE role;
DROP FUNCTION role_guard();
DROP FUNCTION role_for_space_permissions(text[]);
DROP FUNCTION perm_space_closure(text[]);
DELETE FROM groups WHERE builtin IS NOT NULL;
DROP INDEX groups_builtin_idx;
ALTER TABLE groups DROP COLUMN builtin;
ALTER TABLE space DROP CONSTRAINT space_anonymous_team_space;
ALTER TABLE space DROP COLUMN anonymous_view;


ALTER TABLE oidc_provider FORCE ROW LEVEL SECURITY;
ALTER TABLE space FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE ROW LEVEL SECURITY;
ALTER TABLE groups FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;
ALTER TABLE member FORCE ROW LEVEL SECURITY;
