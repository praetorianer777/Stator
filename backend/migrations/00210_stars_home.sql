-- +goose Up
-- Stars and the home page (#38). A star is a person's own mark on a page or a
-- space, to keep it close; it says nothing to anybody else.
CREATE TABLE star (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    user_id    uuid NOT NULL,
    page_id    uuid,
    space_id   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT star_target CHECK ((page_id IS NULL) <> (space_id IS NULL)),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX star_one_per_page_idx ON star (org_id, user_id, page_id) WHERE page_id IS NOT NULL;
CREATE UNIQUE INDEX star_one_per_space_idx ON star (org_id, user_id, space_id) WHERE space_id IS NOT NULL;
-- The list reads a person's stars newest first, a window at a time.
CREATE INDEX star_latest_idx ON star (org_id, user_id, created_at DESC, id DESC);
-- A purged page or a deleted space takes its stars along.
CREATE INDEX star_page_idx ON star (org_id, page_id) WHERE page_id IS NOT NULL;
CREATE INDEX star_space_idx ON star (org_id, space_id) WHERE space_id IS NOT NULL;

ALTER TABLE star ENABLE ROW LEVEL SECURITY;
ALTER TABLE star FORCE  ROW LEVEL SECURITY;

CREATE POLICY star_tenant_isolation ON star
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY star_admin_bypass ON star TO stator_admin USING (true) WITH CHECK (true);

-- A star is only ever one's own, and only on what one may view: a star on a
-- page that became restricted is kept but read by nobody until it opens again.
CREATE POLICY star_own ON star AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id() AND watch_target_viewable(page_id, space_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND watch_target_viewable(page_id, space_id, current_actor_id()));

-- A star is put on or taken off, never turned into another.
GRANT SELECT, INSERT, DELETE ON star TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON star TO stator_admin;

-- When the page's latest version was published. updated_at moves with every
-- move, rank and trash mark, so the feed of updates cannot order by it.
ALTER TABLE page ADD COLUMN published_at timestamptz;

ALTER TABLE page DISABLE TRIGGER USER;
UPDATE page p SET published_at = v.created_at
FROM page_version v
WHERE v.org_id = p.org_id AND v.page_id = p.id AND v.number = p.version;
ALTER TABLE page ENABLE TRIGGER USER;

-- +goose StatementBegin
-- published_at follows the version and nothing else, so nobody moves a page
-- up the feed without publishing it.
CREATE FUNCTION page_published_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSE
        NEW.published_at := OLD.published_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_published_stamp BEFORE INSERT OR UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION page_published_stamp();

-- The feed walks published pages newest first and stops at a window's end.
CREATE INDEX page_published_idx ON page (org_id, published_at DESC, id DESC)
    WHERE published_at IS NOT NULL AND trashed_at IS NULL;
-- What a person edited: their versions, and the pages they made and never published.
CREATE INDEX page_version_author_idx ON page_version (org_id, created_by, page_id, created_at DESC);
CREATE INDEX page_unpublished_idx ON page (org_id, created_by) WHERE version = 0;

-- +goose StatementBegin
-- Pages published by somebody else, out of the trash, that the actor may view,
-- newest first after the keyset (after_at, after_id); with watched, only those
-- the actor's watches cover. The cheap tests run before perm_page_viewable and
-- the walk stops at max rows; row level security would run its policy first,
-- on every row the scan meets.
CREATE FUNCTION home_updates(watched boolean, after_at timestamptz, after_id uuid, max integer)
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
CREATE FUNCTION home_edited(after_at timestamptz, after_id uuid, max integer)
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

REVOKE EXECUTE ON FUNCTION home_updates(boolean, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION home_edited(timestamptz, uuid, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION home_updates(boolean, timestamptz, uuid, integer) TO stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION home_edited(timestamptz, uuid, integer) TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS home_edited(timestamptz, uuid, integer);
DROP FUNCTION IF EXISTS home_updates(boolean, timestamptz, uuid, integer);
DROP INDEX IF EXISTS page_unpublished_idx;
DROP INDEX IF EXISTS page_version_author_idx;
DROP INDEX IF EXISTS page_published_idx;
DROP TRIGGER IF EXISTS page_published_stamp ON page;
DROP FUNCTION IF EXISTS page_published_stamp();
ALTER TABLE page DROP COLUMN IF EXISTS published_at;
DROP TABLE IF EXISTS star;
