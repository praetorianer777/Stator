-- +goose Up
-- Who may do what: global grants, space grants and page restrictions, and
-- policies that hold the app role to them for the person a transaction names.
--
-- The rules are functions, called by the policies and by the service alike,
-- so both answer the same way. Those that read the tables they guard are
-- SECURITY DEFINER: a policy on page that reads page would recurse. They run
-- as the migrations' role, which the restrictive policies here, all TO
-- stator_app, do not bind, and they filter by current_org_id() themselves in
-- case that role bypasses row level security altogether.

-- Composite keys, so a grant names a group of its own organization.
ALTER TABLE groups ADD CONSTRAINT groups_org_id_key UNIQUE (org_id, id);

-- The person the transaction acts for, set with app.org_id. NULL binds the
-- app role to nothing at all, as a missing organization does.
-- +goose StatementBegin
CREATE FUNCTION current_actor_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;
-- +goose StatementEnd

CREATE TABLE global_grant (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    id           uuid NOT NULL DEFAULT uuidv7(),
    -- administer is not granted here: it follows the owner and admin roles.
    permission   text NOT NULL CHECK (permission IN ('use', 'createSpace')),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group', 'everyone')),
    user_id      uuid,
    group_id     uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT global_grant_subject CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL) OR
        (subject_type = 'everyone' AND user_id IS NULL AND group_id IS NULL)),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX global_grant_unique_idx
    ON global_grant (org_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;

CREATE TABLE space_grant (
    org_id       uuid NOT NULL,
    id           uuid NOT NULL DEFAULT uuidv7(),
    space_id     uuid NOT NULL,
    permission   text NOT NULL CHECK (permission IN ('view', 'addPages', 'addComments', 'delete', 'administer')),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group', 'everyone')),
    user_id      uuid,
    group_id     uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT space_grant_subject CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL) OR
        (subject_type = 'everyone' AND user_id IS NULL AND group_id IS NULL)),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX space_grant_unique_idx
    ON space_grant (org_id, space_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;

-- A restriction names people and groups, never everyone: everyone is what
-- the space's own permissions are for.
CREATE TABLE page_restriction (
    org_id       uuid NOT NULL,
    id           uuid NOT NULL DEFAULT uuidv7(),
    page_id      uuid NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('view', 'edit')),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid,
    group_id     uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT page_restriction_subject CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL)),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX page_restriction_unique_idx
    ON page_restriction (org_id, page_id, kind, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX page_restriction_page_idx ON page_restriction (org_id, page_id, kind);

-- +goose StatementBegin
CREATE FUNCTION page_restriction_home_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'view' AND EXISTS (SELECT 1 FROM page WHERE id = NEW.page_id AND parent_id IS NULL) THEN
        RAISE EXCEPTION 'the home page takes no view restriction; narrow the space''s permissions instead'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_restriction_home_viewable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_restriction_home_check BEFORE INSERT OR UPDATE ON page_restriction
    FOR EACH ROW EXECUTE FUNCTION page_restriction_home_guard();

-- The rules.

-- +goose StatementBegin
CREATE FUNCTION perm_subject_matches(actor uuid, subject_type text, user_id uuid, group_id uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT subject_type = 'everyone'
        OR user_id = actor
        OR (group_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM group_member gm
            WHERE gm.org_id = current_org_id() AND gm.group_id = perm_subject_matches.group_id AND gm.user_id = actor))
$$;

CREATE FUNCTION perm_is_member(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM org_member WHERE org_id = current_org_id() AND user_id = actor)
$$;

CREATE FUNCTION perm_is_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE org_id = current_org_id() AND user_id = actor AND org_role IN ('owner', 'admin'))
$$;

-- The global permissions granted to the actor, before administrators are
-- given every one.
CREATE FUNCTION perm_global_grants(actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM global_grant g
    WHERE g.org_id = current_org_id() AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$$;

CREATE FUNCTION perm_global_holds(actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_is_admin(actor)
        OR (permission <> 'administer' AND ARRAY['use', permission] <@ perm_global_grants(actor))
$$;

-- The space permissions granted to the actor, before implication.
CREATE FUNCTION perm_space_grants(actor uuid, space uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g
    WHERE g.org_id = current_org_id() AND g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$$;

-- Every permission implies view and administer implies all; organization
-- administrators hold everything everywhere, and nobody without use holds
-- anything.
CREATE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_is_admin(actor)
        OR (perm_global_holds(actor, 'use') AND EXISTS (
            SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
            WHERE held.p = permission OR held.p = 'administer' OR permission = 'view'))
$$;

-- Whether the actor is on every list of a kind that the pages carry.
CREATE FUNCTION perm_lists_pass(actor uuid, pages uuid[], kind text) RETURNS boolean
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

-- The page and every page above it, the page first.
CREATE FUNCTION perm_page_chain(page_id uuid) RETURNS TABLE (id uuid, space_id uuid, version integer, created_by uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH RECURSIVE up (id, parent_id, space_id, version, created_by, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, 0
        FROM page p WHERE p.id = perm_page_chain.page_id AND p.org_id = current_org_id()
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, up.depth + 1
        FROM page p JOIN up ON p.id = up.parent_id
        WHERE p.org_id = current_org_id()
    )
    SELECT id, space_id, version, created_by FROM up ORDER BY depth
$$;

-- The chain as the service's rules read it, for one person: which pages are
-- hidden drafts, which carry lists and whether the person is on them.
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

-- Viewing a page: the space's view, no unpublished page of somebody else's
-- on the way up, and every view list passed unless the actor administers
-- the space.
CREATE FUNCTION perm_page_viewable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM chain WHERE version = 0 AND created_by IS DISTINCT FROM actor)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), 'view')
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'view'))
$$;

-- Changing a page, or deleting it, is viewing it, holding the space
-- permission, and passing every edit list unless administering the space.
CREATE FUNCTION perm_page_holds(page_id uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT perm_page_viewable(page_id, actor)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), permission)
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))
$$;

CREATE FUNCTION perm_page_editable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT perm_page_holds(page_id, actor, 'addPages')
$$;

CREATE FUNCTION perm_page_deletable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT perm_page_holds(page_id, actor, 'delete')
$$;

-- A new page goes under one the actor may edit. A parent the statement's
-- snapshot does not hold was written by the same statement, as a copied
-- subtree is, and was checked itself.
CREATE FUNCTION perm_page_insertable(space uuid, parent uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN parent IS NULL THEN perm_space_holds(actor, space, 'administer')
        WHEN NOT EXISTS (SELECT 1 FROM page WHERE id = parent AND org_id = current_org_id())
            THEN perm_space_holds(actor, space, 'addPages')
        ELSE perm_page_editable(parent, actor)
    END
$$;
-- +goose StatementEnd

-- Defaults: every organization lets everyone use it, and every space starts
-- as open as spaces were before permissions, administered by its creator.

-- +goose StatementBegin
CREATE FUNCTION org_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    RETURN NEW;
END;
$$;

CREATE FUNCTION space_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO space_grant (org_id, space_id, permission, subject_type)
    SELECT NEW.org_id, NEW.id, p, 'everyone' FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.created_by) THEN
        INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
        VALUES (NEW.org_id, NEW.id, 'administer', 'user', NEW.created_by);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_default_grants AFTER INSERT ON org
    FOR EACH ROW EXECUTE FUNCTION org_default_grants();
CREATE TRIGGER space_default_grants AFTER INSERT ON space
    FOR EACH ROW EXECUTE FUNCTION space_default_grants();

INSERT INTO global_grant (org_id, permission, subject_type)
SELECT id, 'use', 'everyone' FROM org;

INSERT INTO space_grant (org_id, space_id, permission, subject_type)
SELECT s.org_id, s.id, p, 'everyone'
FROM space s CROSS JOIN unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;

INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
SELECT s.org_id, s.id, 'administer', 'user', s.created_by
FROM space s JOIN org_member m ON m.org_id = s.org_id AND m.user_id = s.created_by;

-- Moves of pages the actor cannot see, which trashing, restoring and
-- reordering have to make, go through these functions, each of which checks
-- the rule for the page the actor named. They take the actor from the
-- transaction, never from an argument.

-- +goose StatementBegin
CREATE FUNCTION page_sibling_ranks(parent uuid, skip uuid) RETURNS TABLE (id uuid, rank text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT p.id, p.rank::text FROM page p
    WHERE p.org_id = current_org_id() AND p.parent_id = parent AND p.id IS DISTINCT FROM skip
      AND p.trashed_at IS NULL AND perm_page_viewable(parent, current_actor_id())
    ORDER BY p.rank, p.id
$$;

CREATE FUNCTION page_place(ids uuid[], parent uuid, ranks text[]) RETURNS void
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
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id AND p.org_id = current_org_id();
END;
$$;

CREATE FUNCTION page_trash(root uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF NOT perm_page_deletable(root, actor) THEN
        RAISE EXCEPTION 'you may not delete that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.org_id = current_org_id() AND p.trashed_at IS NULL
    )
    UPDATE page SET trashed_at = now(), trashed_by = actor, trash_id = root
    WHERE org_id = current_org_id() AND id IN (SELECT id FROM below);
END;
$$;

-- Restoring may hang the item under its space's home page, and nowhere else.
CREATE FUNCTION page_untrash(item uuid, home uuid, home_rank text) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item AND org_id = current_org_id())
       OR NOT perm_page_deletable(item, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF home IS NOT NULL THEN
        UPDATE page p SET parent_id = home, rank = home_rank
        FROM space s
        WHERE p.id = item AND p.org_id = current_org_id() AND s.id = p.space_id AND s.home_page_id = home;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'a restored page goes back under its own space''s home page' USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    UPDATE page SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL
    WHERE trash_id = item AND org_id = current_org_id();
END;
$$;

CREATE FUNCTION page_purge(item uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gone bigint;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item AND org_id = current_org_id()
                   AND perm_space_holds(current_actor_id(), space_id, 'administer')) THEN
        RAISE EXCEPTION 'you may not delete that page for good' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE trash_id = item AND org_id = current_org_id();
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$$;

CREATE FUNCTION space_empty_trash(target uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gone bigint;
BEGIN
    IF NOT perm_space_holds(current_actor_id(), target, 'administer') THEN
        RAISE EXCEPTION 'you may not empty that trash' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE space_id = target AND trash_id IS NOT NULL AND org_id = current_org_id();
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$$;
-- +goose StatementEnd

-- A policy cannot tell which columns an UPDATE changes, so this does: content
-- and place need edit, the trash marks need delete. Only the app role is held
-- to it; the functions above and the foreign keys' cascades run as another.
-- +goose StatementBegin
CREATE FUNCTION page_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.body, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank)
       IS DISTINCT FROM (OLD.title, OLD.body, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank)
       AND NOT perm_page_editable(OLD.id, actor) THEN
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
-- +goose StatementEnd

CREATE TRIGGER page_write_check BEFORE UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION page_write_guard();

-- Row level security for the new tables, as for every table.
ALTER TABLE global_grant     ENABLE ROW LEVEL SECURITY;
ALTER TABLE global_grant     FORCE  ROW LEVEL SECURITY;
ALTER TABLE space_grant      ENABLE ROW LEVEL SECURITY;
ALTER TABLE space_grant      FORCE  ROW LEVEL SECURITY;
ALTER TABLE page_restriction ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_restriction FORCE  ROW LEVEL SECURITY;

CREATE POLICY global_grant_tenant_isolation ON global_grant
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY space_grant_tenant_isolation ON space_grant
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_restriction_tenant_isolation ON page_restriction
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY global_grant_admin_bypass ON global_grant TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY space_grant_admin_bypass ON space_grant TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_restriction_admin_bypass ON page_restriction TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON global_grant, space_grant, page_restriction TO stator_app, stator_admin;

-- The roles, for the app role. Restrictive, so they narrow the tenant policy
-- rather than widen it.
CREATE POLICY global_grant_admins ON global_grant AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id())) WITH CHECK (perm_is_admin(current_actor_id()));

CREATE POLICY space_grant_administrators ON space_grant AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'administer'))
    WITH CHECK (perm_space_holds(current_actor_id(), space_id, 'administer'));

CREATE POLICY space_viewers ON space AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_space_holds(current_actor_id(), id, 'view'));
CREATE POLICY space_creators ON space AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_global_holds(current_actor_id(), 'createSpace') AND created_by = current_actor_id());
CREATE POLICY space_administrators ON space AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_space_holds(current_actor_id(), id, 'view'))
    WITH CHECK (perm_space_holds(current_actor_id(), id, 'administer'));
CREATE POLICY space_deleters ON space AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_space_holds(current_actor_id(), id, 'administer'));

CREATE POLICY page_viewers ON page AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(id, current_actor_id()));
CREATE POLICY page_adders ON page AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND perm_page_insertable(space_id, parent_id, current_actor_id()));
-- What an update may change is page_write_guard's to say.
CREATE POLICY page_writers ON page AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_viewable(id, current_actor_id()));
CREATE POLICY page_purgers ON page AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'administer'));

CREATE POLICY page_version_viewers ON page_version AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_version_publishers ON page_version AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_page_editable(page_id, current_actor_id()) AND created_by = current_actor_id());

CREATE POLICY page_draft_own ON page_draft AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id()));

CREATE POLICY page_restriction_viewers ON page_restriction AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_restriction_editors_add ON page_restriction AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_page_editable(page_id, current_actor_id()));
CREATE POLICY page_restriction_editors_change ON page_restriction AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()))
    WITH CHECK (perm_page_editable(page_id, current_actor_id()));
CREATE POLICY page_restriction_editors_remove ON page_restriction AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()));

-- +goose Down
DROP POLICY IF EXISTS page_restriction_editors_remove ON page_restriction;
DROP POLICY IF EXISTS page_restriction_editors_change ON page_restriction;
DROP POLICY IF EXISTS page_restriction_editors_add ON page_restriction;
DROP POLICY IF EXISTS page_restriction_viewers ON page_restriction;
DROP POLICY IF EXISTS page_draft_own ON page_draft;
DROP POLICY IF EXISTS page_version_publishers ON page_version;
DROP POLICY IF EXISTS page_version_viewers ON page_version;
DROP POLICY IF EXISTS page_purgers ON page;
DROP POLICY IF EXISTS page_writers ON page;
DROP POLICY IF EXISTS page_adders ON page;
DROP POLICY IF EXISTS page_viewers ON page;
DROP POLICY IF EXISTS space_deleters ON space;
DROP POLICY IF EXISTS space_administrators ON space;
DROP POLICY IF EXISTS space_creators ON space;
DROP POLICY IF EXISTS space_viewers ON space;
DROP TRIGGER IF EXISTS page_write_check ON page;
DROP FUNCTION IF EXISTS page_write_guard();
DROP TRIGGER IF EXISTS space_default_grants ON space;
DROP TRIGGER IF EXISTS org_default_grants ON org;
DROP FUNCTION IF EXISTS space_empty_trash(uuid);
DROP FUNCTION IF EXISTS page_purge(uuid);
DROP FUNCTION IF EXISTS page_untrash(uuid, uuid, text);
DROP FUNCTION IF EXISTS page_trash(uuid);
DROP FUNCTION IF EXISTS page_place(uuid[], uuid, text[]);
DROP FUNCTION IF EXISTS page_sibling_ranks(uuid, uuid);
DROP FUNCTION IF EXISTS space_default_grants();
DROP FUNCTION IF EXISTS org_default_grants();
DROP TABLE IF EXISTS page_restriction, space_grant, global_grant;
DROP FUNCTION IF EXISTS page_restriction_home_guard();
DROP FUNCTION IF EXISTS perm_page_insertable(uuid, uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_deletable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_editable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_holds(uuid, uuid, text);
DROP FUNCTION IF EXISTS perm_page_viewable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_lists(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_chain(uuid);
DROP FUNCTION IF EXISTS perm_lists_pass(uuid, uuid[], text);
DROP FUNCTION IF EXISTS perm_space_holds(uuid, uuid, text);
DROP FUNCTION IF EXISTS perm_space_grants(uuid, uuid);
DROP FUNCTION IF EXISTS perm_global_holds(uuid, text);
DROP FUNCTION IF EXISTS perm_global_grants(uuid);
DROP FUNCTION IF EXISTS perm_is_admin(uuid);
DROP FUNCTION IF EXISTS perm_is_member(uuid);
DROP FUNCTION IF EXISTS perm_subject_matches(uuid, text, uuid, uuid);
DROP FUNCTION IF EXISTS current_actor_id();
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_org_id_key;
