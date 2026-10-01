-- +goose Up
-- Archiving (#37). An archived page, with every page below it that was in the
-- tree, is one item of its space's archive, named by archive_id, the page
-- archived; an archived space is one flag. Both stay readable and linkable,
-- and nothing in them changes until they are unarchived.
ALTER TABLE space
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by uuid REFERENCES app_user(id) ON DELETE SET NULL;

ALTER TABLE page
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    ADD COLUMN archive_id  uuid,
    ADD CONSTRAINT page_archive_whole CHECK ((archived_at IS NULL) = (archive_id IS NULL)),
    -- The home page stands for its space, which is archived as a whole.
    ADD CONSTRAINT page_home_never_archived CHECK (parent_id IS NOT NULL OR archived_at IS NULL),
    ADD CONSTRAINT page_archive_fkey FOREIGN KEY (org_id, archive_id) REFERENCES page (org_id, id) ON DELETE CASCADE;

CREATE INDEX page_archive_roots_idx ON page (org_id, space_id, archived_at DESC) WHERE archive_id = id;
CREATE INDEX page_archive_items_idx ON page (org_id, archive_id) WHERE archive_id IS NOT NULL;

-- +goose StatementBegin
-- Whether a page is archived, by an item of its own or with its space.
CREATE FUNCTION page_archived(target uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target AND p.org_id = current_org_id()
          AND (p.archived_at IS NOT NULL OR s.archived_at IS NOT NULL))
$$;

-- As in 00090, and nothing of an archived page changes: every policy and
-- guard that asks for edit or delete refuses it, until it is unarchived.
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

-- As in 00140, and an archived page takes no comments, reactions or passages.
CREATE OR REPLACE FUNCTION perm_page_commentable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.version > 0 AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addComments'))
       AND NOT page_archived(target)
$$;

-- Archiving and unarchiving are for whoever administers the space and may
-- view the page, as in Armature a project's administrators archive it.
CREATE FUNCTION perm_page_archivable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL AND s.archived_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'administer'))
$$;

-- Marks the page and every page below it still in the tree and not archived
-- already, those the actor cannot see included, as one item. A page archived
-- earlier below it stays an item of its own. Archiving twice is no change.
CREATE FUNCTION page_archive(root uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    marked bigint;
BEGIN
    IF NOT perm_page_archivable(root, actor) THEN
        RAISE EXCEPTION 'you may not archive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page WHERE id = root AND org_id = current_org_id() AND archived_at IS NOT NULL) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.org_id = current_org_id() AND p.trashed_at IS NULL AND p.archived_at IS NULL
    )
    UPDATE page SET archived_at = now(), archived_by = actor, archive_id = root
    WHERE org_id = current_org_id() AND id IN (SELECT id FROM below);
    GET DIAGNOSTICS marked = ROW_COUNT;
    RETURN marked;
END;
$$;

-- Clears an item. Only the page archived may be unarchived, and only while
-- the page above it is not archived, so no page that may change ever hangs
-- under one that may not.
CREATE FUNCTION page_unarchive(item uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    cleared bigint;
BEGIN
    IF NOT perm_page_archivable(item, current_actor_id())
       OR NOT EXISTS (SELECT 1 FROM page WHERE id = item AND archive_id = item AND org_id = current_org_id()) THEN
        RAISE EXCEPTION 'you may not unarchive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN page up ON up.id = p.parent_id
               WHERE p.id = item AND up.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'the page above it is archived; unarchive that one first' USING ERRCODE = 'check_violation',
            CONSTRAINT = 'page_archive_parent_first';
    END IF;
    UPDATE page SET archived_at = NULL, archived_by = NULL, archive_id = NULL
    WHERE archive_id = item AND org_id = current_org_id();
    GET DIAGNOSTICS cleared = ROW_COUNT;
    RETURN cleared;
END;
$$;

-- As in 00090, but nothing moves under an archived page, and an archived page
-- moves only with the page its item hangs from, even for an administrator.
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

-- The archive marks are page_archive's and page_unarchive's to write, and a
-- new page is never born archived.
CREATE FUNCTION page_archive_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND NEW.archived_at IS NOT NULL
       OR TG_OP = 'UPDATE' AND (NEW.archived_at, NEW.archived_by, NEW.archive_id)
                               IS DISTINCT FROM (OLD.archived_at, OLD.archived_by, OLD.archive_id) THEN
        RAISE EXCEPTION 'pages are archived and unarchived through page_archive and page_unarchive'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- As in 00141, and archiving or unarchiving a page is not a change of it.
CREATE OR REPLACE FUNCTION page_touch() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.archived_at, NEW.archive_id) IS DISTINCT FROM (OLD.archived_at, OLD.archive_id)
       AND (NEW.title, NEW.body, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank, NEW.trashed_at)
           IS NOT DISTINCT FROM (OLD.title, OLD.body, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank, OLD.trashed_at) THEN
        NEW.updated_at := OLD.updated_at;
    ELSIF NEW.body IS DISTINCT FROM OLD.body
       AND (NEW.title, NEW.version) IS NOT DISTINCT FROM (OLD.title, OLD.version)
       AND document_without_anchors(NEW.body, NULL) = document_without_anchors(OLD.body, NULL) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

-- When and by whom a space was archived are the database's to say, so they
-- cannot be backdated or put in somebody else's name.
CREATE FUNCTION space_archive_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.archived_at IS NULL) = (OLD.archived_at IS NULL) THEN
        NEW.archived_at := OLD.archived_at;
        NEW.archived_by := OLD.archived_by;
    ELSIF NEW.archived_at IS NULL THEN
        NEW.archived_by := NULL;
    ELSE
        NEW.archived_at := now();
        NEW.archived_by := current_actor_id();
    END IF;
    RETURN NEW;
END;
$$;

-- What the home page lists leaves archived pages and spaces out: they are
-- done with, and found again through their space's archive or search.
CREATE OR REPLACE FUNCTION home_updates(watched boolean, after_at timestamptz, after_id uuid, max integer)
    RETURNS TABLE (page_id uuid, title text, space_key text, space_name text, version integer,
                   published_at timestamptz, author_id uuid, author_name text, comment text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH viewable_spaces AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.org_id = current_org_id() AND s.archived_at IS NULL AND perm_space_holds(current_actor_id(), s.id, 'view')
    ), watched_spaces AS MATERIALIZED (
        SELECT w.space_id AS id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'space'
    ), watched_pages AS MATERIALIZED (
        WITH RECURSIVE below (id) AS (
            SELECT w.page_id FROM watch w
            WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'subtree'
            UNION
            SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
            WHERE p.org_id = current_org_id()
        )
        SELECT id FROM below
        UNION
        SELECT w.page_id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'page'
    ), picked AS (
        SELECT p.id, p.title, p.space_id, p.version, p.published_at, p.updated_by
        FROM page p
        WHERE p.org_id = current_org_id() AND p.published_at IS NOT NULL AND p.trashed_at IS NULL AND p.archived_at IS NULL
          AND (after_at IS NULL OR (p.published_at, p.id) < (after_at, after_id))
          AND p.updated_by IS DISTINCT FROM current_actor_id()
          -- A CASE fixes the order; as plain conditions the planner joined the
          -- spaces after the view rule and ran it on every hidden page it met.
          AND CASE WHEN p.space_id IN (SELECT id FROM viewable_spaces)
                        AND (NOT watched OR p.space_id IN (SELECT id FROM watched_spaces)
                             OR p.id IN (SELECT id FROM watched_pages))
                   THEN perm_page_viewable(p.id, current_actor_id())
                   ELSE false END
        ORDER BY p.published_at DESC, p.id DESC
        LIMIT max
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.updated_by, COALESCE(u.name, ''), COALESCE(v.comment, '')
    FROM picked k
    JOIN space s ON s.id = k.space_id
    LEFT JOIN app_user u ON u.id = k.updated_by
    LEFT JOIN page_version v ON v.org_id = current_org_id() AND v.page_id = k.id AND v.number = k.version
    ORDER BY k.published_at DESC, k.id DESC
$$;

CREATE OR REPLACE FUNCTION home_edited(after_at timestamptz, after_id uuid, max integer)
    RETURNS TABLE (page_id uuid, title text, space_key text, space_name text, edited_at timestamptz,
                   draft boolean, unpublished boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH touched (page_id, at, draft) AS (
        SELECT v.page_id, max(v.created_at), false FROM page_version v
        WHERE v.org_id = current_org_id() AND v.created_by = current_actor_id()
        GROUP BY v.page_id
        UNION ALL
        SELECT d.page_id, d.updated_at, true FROM page_draft d
        WHERE d.org_id = current_org_id() AND d.user_id = current_actor_id()
        UNION ALL
        SELECT p.id, p.updated_at, true FROM page p
        WHERE p.org_id = current_org_id() AND p.created_by = current_actor_id() AND p.version = 0
    ), latest AS (
        SELECT t.page_id, max(t.at) AS at, bool_or(t.draft) AS draft FROM touched t GROUP BY t.page_id
    ), picked AS (
        SELECT l.page_id, l.at, l.draft, p.title, p.space_id, p.version
        FROM latest l
        JOIN page p ON p.org_id = current_org_id() AND p.id = l.page_id
        JOIN space s ON s.id = p.space_id
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL AND s.archived_at IS NULL
          AND (after_at IS NULL OR (l.at, l.page_id) < (after_at, after_id))
          AND perm_page_viewable(p.id, current_actor_id())
        ORDER BY l.at DESC, l.page_id DESC
        LIMIT max
    )
    SELECT k.page_id, k.title, s.key, s.name, k.at, k.draft, k.version = 0
    FROM picked k JOIN space s ON s.id = k.space_id
    ORDER BY k.at DESC, k.page_id DESC
$$;
-- +goose StatementEnd

CREATE TRIGGER page_archive_check BEFORE INSERT OR UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION page_archive_guard();
CREATE TRIGGER space_archive_stamp BEFORE UPDATE ON space
    FOR EACH ROW EXECUTE FUNCTION space_archive_stamp();

REVOKE EXECUTE ON FUNCTION page_archive(uuid), page_unarchive(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_archive(uuid), page_unarchive(uuid) TO stator_app, stator_admin;

-- +goose Down
DROP TRIGGER IF EXISTS space_archive_stamp ON space;
DROP TRIGGER IF EXISTS page_archive_check ON page;
DROP FUNCTION IF EXISTS space_archive_stamp();
DROP FUNCTION IF EXISTS page_archive_guard();
DROP FUNCTION IF EXISTS page_unarchive(uuid);
DROP FUNCTION IF EXISTS page_archive(uuid);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_touch() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.body IS DISTINCT FROM OLD.body
       AND (NEW.title, NEW.version) IS NOT DISTINCT FROM (OLD.title, OLD.version)
       AND document_without_anchors(NEW.body, NULL) = document_without_anchors(OLD.body, NULL) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
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
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id AND p.org_id = current_org_id();
END;
$$;

CREATE OR REPLACE FUNCTION perm_page_commentable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.version > 0 AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addComments'))
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
$$;

CREATE OR REPLACE FUNCTION home_updates(watched boolean, after_at timestamptz, after_id uuid, max integer)
    RETURNS TABLE (page_id uuid, title text, space_key text, space_name text, version integer,
                   published_at timestamptz, author_id uuid, author_name text, comment text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH viewable_spaces AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.org_id = current_org_id() AND perm_space_holds(current_actor_id(), s.id, 'view')
    ), watched_spaces AS MATERIALIZED (
        SELECT w.space_id AS id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'space'
    ), watched_pages AS MATERIALIZED (
        WITH RECURSIVE below (id) AS (
            SELECT w.page_id FROM watch w
            WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'subtree'
            UNION
            SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
            WHERE p.org_id = current_org_id()
        )
        SELECT id FROM below
        UNION
        SELECT w.page_id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'page'
    ), picked AS (
        SELECT p.id, p.title, p.space_id, p.version, p.published_at, p.updated_by
        FROM page p
        WHERE p.org_id = current_org_id() AND p.published_at IS NOT NULL AND p.trashed_at IS NULL
          AND (after_at IS NULL OR (p.published_at, p.id) < (after_at, after_id))
          AND p.updated_by IS DISTINCT FROM current_actor_id()
          -- A CASE fixes the order; as plain conditions the planner joined the
          -- spaces after the view rule and ran it on every hidden page it met.
          AND CASE WHEN p.space_id IN (SELECT id FROM viewable_spaces)
                        AND (NOT watched OR p.space_id IN (SELECT id FROM watched_spaces)
                             OR p.id IN (SELECT id FROM watched_pages))
                   THEN perm_page_viewable(p.id, current_actor_id())
                   ELSE false END
        ORDER BY p.published_at DESC, p.id DESC
        LIMIT max
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.updated_by, COALESCE(u.name, ''), COALESCE(v.comment, '')
    FROM picked k
    JOIN space s ON s.id = k.space_id
    LEFT JOIN app_user u ON u.id = k.updated_by
    LEFT JOIN page_version v ON v.org_id = current_org_id() AND v.page_id = k.id AND v.number = k.version
    ORDER BY k.published_at DESC, k.id DESC
$$;

-- What the actor edited, newest first after the keyset: pages they published a
-- version of, pages they hold a draft of, and pages they made and never
-- published, out of the trash and only while they may view them.
CREATE OR REPLACE FUNCTION home_edited(after_at timestamptz, after_id uuid, max integer)
    RETURNS TABLE (page_id uuid, title text, space_key text, space_name text, edited_at timestamptz,
                   draft boolean, unpublished boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH touched (page_id, at, draft) AS (
        SELECT v.page_id, max(v.created_at), false FROM page_version v
        WHERE v.org_id = current_org_id() AND v.created_by = current_actor_id()
        GROUP BY v.page_id
        UNION ALL
        SELECT d.page_id, d.updated_at, true FROM page_draft d
        WHERE d.org_id = current_org_id() AND d.user_id = current_actor_id()
        UNION ALL
        SELECT p.id, p.updated_at, true FROM page p
        WHERE p.org_id = current_org_id() AND p.created_by = current_actor_id() AND p.version = 0
    ), latest AS (
        SELECT t.page_id, max(t.at) AS at, bool_or(t.draft) AS draft FROM touched t GROUP BY t.page_id
    ), picked AS (
        SELECT l.page_id, l.at, l.draft, p.title, p.space_id, p.version
        FROM latest l JOIN page p ON p.org_id = current_org_id() AND p.id = l.page_id
        WHERE p.trashed_at IS NULL
          AND (after_at IS NULL OR (l.at, l.page_id) < (after_at, after_id))
          AND perm_page_viewable(p.id, current_actor_id())
        ORDER BY l.at DESC, l.page_id DESC
        LIMIT max
    )
    SELECT k.page_id, k.title, s.key, s.name, k.at, k.draft, k.version = 0
    FROM picked k JOIN space s ON s.id = k.space_id
    ORDER BY k.at DESC, k.page_id DESC
$$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS perm_page_archivable(uuid, uuid);
DROP FUNCTION IF EXISTS page_archived(uuid);
DROP INDEX IF EXISTS page_archive_items_idx;
DROP INDEX IF EXISTS page_archive_roots_idx;
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_archive_fkey,
    DROP CONSTRAINT IF EXISTS page_home_never_archived,
    DROP CONSTRAINT IF EXISTS page_archive_whole,
    DROP COLUMN IF EXISTS archive_id,
    DROP COLUMN IF EXISTS archived_by,
    DROP COLUMN IF EXISTS archived_at;
ALTER TABLE space
    DROP COLUMN IF EXISTS archived_by,
    DROP COLUMN IF EXISTS archived_at;
