-- +goose Up
-- Also allowed to edit (#304): a third list on a page, of people and groups
-- who may edit it and the pages below it although the space does not let
-- them add pages. It only ever widens editing. Whoever it names must still
-- view the page and pass every edit list on it and above it, where an entry
-- counts as one of its own page's edit list too. Adding, moving and copying
-- pages keep needing add pages, and only administrators of the space change
-- the list, since it widens what the space's permissions allow.
ALTER TABLE page_restriction
    DROP CONSTRAINT page_restriction_kind_check,
    ADD CONSTRAINT page_restriction_kind_check CHECK (kind IN ('view', 'edit', 'editGrant'));

-- +goose StatementBegin
-- As in 00090, and an entry of a page's grant list passes that page's edit
-- list as well: the one list is the other's plus the space's add pages.
CREATE OR REPLACE FUNCTION perm_lists_pass(actor uuid, pages uuid[], kind text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT EXISTS (
        SELECT 1 FROM unnest(pages) AS up (id)
        WHERE EXISTS (SELECT 1 FROM page_restriction r
                      WHERE r.org_id = current_org_id() AND r.page_id = up.id AND r.kind = perm_lists_pass.kind)
          AND NOT EXISTS (SELECT 1 FROM page_restriction r
                          WHERE r.org_id = current_org_id() AND r.page_id = up.id
                            AND (r.kind = perm_lists_pass.kind OR (perm_lists_pass.kind = 'edit' AND r.kind = 'editGrant'))
                            AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)))
$$;

-- Whether a grant list on any of the pages names the actor.
CREATE FUNCTION perm_grants_name(actor uuid, pages uuid[]) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_restriction r
        WHERE r.org_id = current_org_id() AND r.page_id = ANY (pages) AND r.kind = 'editGrant'
          AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id))
$$;

-- As in 00260, and editing is also open to whoever a grant list on the page
-- or above it names, with every other condition as it was.
CREATE OR REPLACE FUNCTION perm_page_holds(page_id uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT perm_page_viewable(page_id, actor)
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR ((perm_space_holds(actor, (SELECT space_id FROM sp), permission)
                 OR (permission = 'addPages' AND perm_grants_name(actor, ARRAY(SELECT id FROM chain))))
                AND perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit')))
       AND NOT page_archived(page_id)
$$;

-- Adding a page below one, or moving pages under it or among its children,
-- is editing it and holding add pages in its space, which a grant never gives.
CREATE FUNCTION perm_page_arrangeable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_editable(target, actor) AND EXISTS (
        SELECT 1 FROM page p WHERE p.id = target AND p.org_id = current_org_id()
          AND perm_space_holds(actor, p.space_id, 'addPages'))
$$;

-- As in 00090, but the parent must be arrangeable, not merely editable.
CREATE OR REPLACE FUNCTION perm_page_insertable(space uuid, parent uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN parent IS NULL THEN perm_space_holds(actor, space, 'administer')
        WHEN NOT EXISTS (SELECT 1 FROM page WHERE id = parent AND org_id = current_org_id())
            THEN perm_space_holds(actor, space, 'addPages')
        ELSE perm_page_arrangeable(parent, actor)
    END
$$;

-- As in 00260, with arrangeable for editable.
CREATE OR REPLACE FUNCTION page_place(ids uuid[], parent uuid, ranks text[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    sp uuid := (SELECT space_id FROM page WHERE id = parent AND org_id = current_org_id());
BEGIN
    IF sp IS NULL OR NOT (perm_space_holds(actor, sp, 'administer') OR (
        perm_page_arrangeable(parent, actor) AND NOT EXISTS (
            SELECT 1 FROM page p WHERE p.id = ANY (ids) AND p.org_id = current_org_id()
              AND NOT perm_page_arrangeable(p.parent_id, actor)))) THEN
        RAISE EXCEPTION 'you may not rearrange the pages there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
               WHERE p.id = ANY (ids) AND p.org_id = current_org_id() AND p.parent_id IS DISTINCT FROM parent
                 AND (page_archived(parent) OR s.archived_at IS NOT NULL OR p.archive_id <> p.id)) THEN
        RAISE EXCEPTION 'archived pages stay where they are' USING ERRCODE = 'insufficient_privilege';
    END IF;
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id AND p.org_id = current_org_id();
END;
$$;

-- As in 00141, and a page's place, its parent, space and rank among its
-- siblings, changes only for whoever may arrange it.
CREATE OR REPLACE FUNCTION page_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.version) IS DISTINCT FROM (OLD.title, OLD.version)
       AND NOT perm_page_editable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.parent_id, NEW.space_id, NEW.rank) IS DISTINCT FROM (OLD.parent_id, OLD.space_id, OLD.rank)
       AND NOT perm_page_arrangeable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not move that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.body IS DISTINCT FROM OLD.body AND NOT perm_page_editable(OLD.id, actor)
       AND NOT page_anchor_added(OLD.id, OLD.body, NEW.body, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id AND NOT perm_page_arrangeable(NEW.parent_id, actor) THEN
        RAISE EXCEPTION 'you may not put a page there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.trashed_at, NEW.trash_id, NEW.trashed_by) IS DISTINCT FROM (OLD.trashed_at, OLD.trash_id, OLD.trashed_by)
       AND NOT perm_page_deletable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not delete or restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- As in 00300: seeing who read a page follows whether one may change it,
-- grant included, though an archived page still answers.
CREATE OR REPLACE FUNCTION perm_page_readers_listable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT c.id FROM perm_page_chain(target) c)
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL
          AND (perm_space_holds(actor, p.space_id, 'administer')
               OR ((perm_space_holds(actor, p.space_id, 'addPages') OR perm_grants_name(actor, ARRAY(SELECT id FROM chain)))
                   AND perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))))
$$;

-- Whether the actor may change who a page's grant list names: administering
-- its space, as the space's own permission table takes.
CREATE FUNCTION perm_page_grantable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.org_id = current_org_id()
                     AND perm_space_holds(actor, p.space_id, 'administer'))
$$;

-- A guest is granted in their own space only, as space_grant_guest_guard
-- holds their space grants.
CREATE FUNCTION page_edit_grant_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gs uuid;
BEGIN
    IF NEW.kind <> 'editGrant' OR NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
    IF gs IS NOT NULL AND gs IS DISTINCT FROM (SELECT space_id FROM page WHERE id = NEW.page_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to, so they cannot edit a page of another. Pick somebody else, or make them a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_edit_grant_guest_one_space';
    END IF;
    RETURN NEW;
END;
$$;

-- A grant is the word of the space's administrators about their space, so a
-- page moved to another space leaves its grant list behind.
CREATE FUNCTION page_edit_grants_stay() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM page_restriction WHERE org_id = NEW.org_id AND page_id = NEW.id AND kind = 'editGrant';
    RETURN NULL;
END;
$$;

-- The people a page's edit list would name who could not edit it for want
-- of add pages in its space, with the page's grant list as given and those
-- above it as stored; a group's members each, with the group. For whoever
-- may edit the page, which the restrictions dialog asks before saving.
CREATE FUNCTION page_edit_blocked(target uuid, edit_users uuid[], edit_groups uuid[], grant_users uuid[], grant_groups uuid[])
    RETURNS TABLE (user_id uuid, group_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH sp AS (SELECT p.space_id FROM page p WHERE p.id = target AND p.org_id = current_org_id()),
         above AS (SELECT c.id FROM perm_page_chain(target) c WHERE c.id <> target),
         named (user_id, group_id) AS (
             SELECT u, NULL::uuid FROM unnest(edit_users) AS u
             UNION
             SELECT gm.user_id, gm.group_id FROM group_member gm
             WHERE gm.org_id = current_org_id() AND gm.group_id = ANY (edit_groups))
    SELECT n.user_id, n.group_id FROM named n
    WHERE perm_page_editable(target, current_actor_id())
      AND NOT perm_space_holds(n.user_id, (SELECT space_id FROM sp), 'addPages')
      AND NOT n.user_id = ANY (COALESCE(grant_users, '{}'))
      AND NOT EXISTS (SELECT 1 FROM group_member gm
                      WHERE gm.org_id = current_org_id() AND gm.user_id = n.user_id AND gm.group_id = ANY (COALESCE(grant_groups, '{}')))
      AND NOT perm_grants_name(n.user_id, ARRAY(SELECT id FROM above))
    ORDER BY n.group_id NULLS FIRST, n.user_id
$$;
-- +goose StatementEnd

-- perm_page_lists answers whether a grant list names the person on each page.
DROP FUNCTION perm_page_lists(uuid, uuid);
-- +goose StatementBegin
CREATE FUNCTION perm_page_lists(page_id uuid, actor uuid)
    RETURNS TABLE (id uuid, space_id uuid, hidden boolean,
                   view_listed boolean, on_view_list boolean, edit_listed boolean, on_edit_list boolean, granted boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.id, c.space_id, c.version = 0 AND c.created_by IS DISTINCT FROM actor,
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           perm_grants_name(actor, ARRAY[c.id])
    FROM perm_page_chain(page_id) c
$$;
-- +goose StatementEnd

CREATE TRIGGER page_edit_grant_guest_check BEFORE INSERT OR UPDATE ON page_restriction
    FOR EACH ROW EXECUTE FUNCTION page_edit_grant_guest_guard();
CREATE TRIGGER page_edit_grants_stay AFTER UPDATE OF space_id ON page
    FOR EACH ROW WHEN (NEW.space_id IS DISTINCT FROM OLD.space_id)
    EXECUTE FUNCTION page_edit_grants_stay();

CREATE POLICY page_edit_grants_administrators_add ON page_restriction AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (kind <> 'editGrant' OR perm_page_grantable(page_id, current_actor_id()));
CREATE POLICY page_edit_grants_administrators_change ON page_restriction AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (kind <> 'editGrant' OR perm_page_grantable(page_id, current_actor_id()))
    WITH CHECK (kind <> 'editGrant' OR perm_page_grantable(page_id, current_actor_id()));
CREATE POLICY page_edit_grants_administrators_remove ON page_restriction AS RESTRICTIVE FOR DELETE TO stator_app
    USING (kind <> 'editGrant' OR perm_page_grantable(page_id, current_actor_id()));

-- +goose Down
DROP POLICY IF EXISTS page_edit_grants_administrators_remove ON page_restriction;
DROP POLICY IF EXISTS page_edit_grants_administrators_change ON page_restriction;
DROP POLICY IF EXISTS page_edit_grants_administrators_add ON page_restriction;
DROP TRIGGER IF EXISTS page_edit_grants_stay ON page;
DROP TRIGGER IF EXISTS page_edit_grant_guest_check ON page_restriction;
DELETE FROM page_restriction WHERE kind = 'editGrant';
ALTER TABLE page_restriction
    DROP CONSTRAINT page_restriction_kind_check,
    ADD CONSTRAINT page_restriction_kind_check CHECK (kind IN ('view', 'edit'));

DROP FUNCTION IF EXISTS perm_page_lists(uuid, uuid);
-- +goose StatementBegin
CREATE FUNCTION perm_page_lists(page_id uuid, actor uuid)
    RETURNS TABLE (id uuid, space_id uuid, hidden boolean,
                   view_listed boolean, on_view_list boolean, edit_listed boolean, on_edit_list boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.id, c.space_id, c.version = 0 AND c.created_by IS DISTINCT FROM actor,
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id))
    FROM perm_page_chain(page_id) c
$$;

CREATE OR REPLACE FUNCTION perm_page_readers_listable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addPages')
          AND (perm_space_holds(actor, p.space_id, 'administer')
               OR perm_lists_pass(actor, ARRAY(SELECT c.id FROM perm_page_chain(target) c), 'edit')))
$$;

CREATE OR REPLACE FUNCTION page_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank)
       IS DISTINCT FROM (OLD.title, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank)
       AND NOT perm_page_editable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.body IS DISTINCT FROM OLD.body AND NOT perm_page_editable(OLD.id, actor)
       AND NOT page_anchor_added(OLD.id, OLD.body, NEW.body, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id AND NOT perm_page_editable(NEW.parent_id, actor) THEN
        RAISE EXCEPTION 'you may not put a page there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.trashed_at, NEW.trash_id, NEW.trashed_by) IS DISTINCT FROM (OLD.trashed_at, OLD.trash_id, OLD.trashed_by)
       AND NOT perm_page_deletable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not delete or restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION page_place(ids uuid[], parent uuid, ranks text[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    sp uuid := (SELECT space_id FROM page WHERE id = parent AND org_id = current_org_id());
BEGIN
    IF sp IS NULL OR NOT (perm_space_holds(actor, sp, 'administer') OR (
        perm_page_editable(parent, actor) AND NOT EXISTS (
            SELECT 1 FROM page p WHERE p.id = ANY (ids) AND p.org_id = current_org_id()
              AND NOT perm_page_editable(p.parent_id, actor)))) THEN
        RAISE EXCEPTION 'you may not rearrange the pages there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
               WHERE p.id = ANY (ids) AND p.org_id = current_org_id() AND p.parent_id IS DISTINCT FROM parent
                 AND (page_archived(parent) OR s.archived_at IS NOT NULL OR p.archive_id <> p.id)) THEN
        RAISE EXCEPTION 'archived pages stay where they are' USING ERRCODE = 'insufficient_privilege';
    END IF;
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id AND p.org_id = current_org_id();
END;
$$;

CREATE OR REPLACE FUNCTION perm_page_insertable(space uuid, parent uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN parent IS NULL THEN perm_space_holds(actor, space, 'administer')
        WHEN NOT EXISTS (SELECT 1 FROM page WHERE id = parent AND org_id = current_org_id())
            THEN perm_space_holds(actor, space, 'addPages')
        ELSE perm_page_editable(parent, actor)
    END
$$;

CREATE OR REPLACE FUNCTION perm_page_holds(page_id uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT perm_page_viewable(page_id, actor)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), permission)
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))
       AND NOT page_archived(page_id)
$$;

CREATE OR REPLACE FUNCTION perm_lists_pass(actor uuid, pages uuid[], kind text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT EXISTS (
        SELECT 1 FROM unnest(pages) AS up (id)
        WHERE EXISTS (SELECT 1 FROM page_restriction r
                      WHERE r.org_id = current_org_id() AND r.page_id = up.id AND r.kind = perm_lists_pass.kind)
          AND NOT EXISTS (SELECT 1 FROM page_restriction r
                          WHERE r.org_id = current_org_id() AND r.page_id = up.id AND r.kind = perm_lists_pass.kind
                            AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)))
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS page_edit_blocked(uuid, uuid[], uuid[], uuid[], uuid[]);
DROP FUNCTION IF EXISTS page_edit_grants_stay();
DROP FUNCTION IF EXISTS page_edit_grant_guest_guard();
DROP FUNCTION IF EXISTS perm_page_grantable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_arrangeable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_grants_name(uuid, uuid[]);
