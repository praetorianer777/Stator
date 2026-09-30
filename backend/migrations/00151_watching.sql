-- +goose Up
-- Who follows what. A watch is on one page, on a page and everything below it
-- (subtree), or on a whole space. What is below a page and which space it is
-- in are read when an event is delivered, so a watch stores neither: a moved
-- page is covered by its new place's watches.
CREATE TABLE watch (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    user_id    uuid NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('page', 'subtree', 'space')),
    page_id    uuid,
    space_id   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT watch_target CHECK (
        (kind = 'space' AND space_id IS NOT NULL AND page_id IS NULL) OR
        (kind <> 'space' AND page_id IS NOT NULL AND space_id IS NULL)),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE
);

-- One own watch per page and per space; a new one replaces the old.
CREATE UNIQUE INDEX watch_one_per_page_idx ON watch (org_id, user_id, page_id) WHERE page_id IS NOT NULL;
CREATE UNIQUE INDEX watch_one_per_space_idx ON watch (org_id, user_id, space_id) WHERE space_id IS NOT NULL;
-- Delivery looks watches up by what they are on.
CREATE INDEX watch_page_idx ON watch (org_id, page_id) WHERE page_id IS NOT NULL;
CREATE INDEX watch_space_idx ON watch (org_id, space_id) WHERE space_id IS NOT NULL;
CREATE INDEX watch_latest_idx ON watch (org_id, user_id, created_at DESC);

-- Somebody who stopped watching a page: their own edits no longer make them
-- watch it again, until they watch it explicitly.
CREATE TABLE watch_optout (
    org_id     uuid NOT NULL,
    user_id    uuid NOT NULL,
    page_id    uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id, page_id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

-- +goose StatementBegin
-- Whether the actor may view what a watch is on.
CREATE FUNCTION watch_target_viewable(page uuid, space uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT CASE WHEN page IS NOT NULL THEN perm_page_viewable(page, actor)
                ELSE perm_space_holds(actor, space, 'view') END
$$;

-- Every person whose watch covers a page, through the nearest watch: their
-- own on the page, a subtree watch above, or the space's. with_parent also
-- counts a watch on the page directly above, for a page first published
-- under it. Nobody's visibility is judged here; the caller does that for
-- each person. Read across people, so it is the worker's alone.
CREATE FUNCTION page_watch_coverage(target uuid, with_parent boolean)
    RETURNS TABLE (user_id uuid, via text, via_page uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH RECURSIVE up (id, parent_id, space_id, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, 0 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id()
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, up.depth + 1 FROM page p JOIN up ON p.id = up.parent_id
        WHERE p.org_id = current_org_id()
    ), found (user_id, via, via_page, depth) AS (
        SELECT w.user_id, w.kind, CASE WHEN w.kind = 'subtree' OR up.depth > 0 THEN w.page_id END, up.depth
        FROM watch w JOIN up ON w.page_id = up.id
        WHERE w.org_id = current_org_id()
          AND (up.depth = 0 OR w.kind = 'subtree' OR (with_parent AND up.depth = 1))
        UNION ALL
        SELECT w.user_id, 'space', NULL, 2147483647
        FROM watch w
        WHERE w.org_id = current_org_id() AND w.kind = 'space'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0)
    )
    SELECT DISTINCT ON (f.user_id) f.user_id, f.via, f.via_page
    FROM found f
    ORDER BY f.user_id, f.depth
$$;

-- Who hears about a new version of a page, for somebody who may view it:
-- only people who may view it themselves.
CREATE FUNCTION page_watchers(target uuid)
    RETURNS TABLE (user_id uuid, via text, via_page uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.user_id, c.via, c.via_page
    FROM page_watch_coverage(target, false) c
    WHERE perm_page_viewable(target, current_actor_id())
      AND perm_page_viewable(target, c.user_id)
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION page_watch_coverage(uuid, boolean) FROM PUBLIC, stator_app;
GRANT EXECUTE ON FUNCTION page_watch_coverage(uuid, boolean) TO stator_admin;

ALTER TABLE watch        ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch        FORCE  ROW LEVEL SECURITY;
ALTER TABLE watch_optout ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch_optout FORCE  ROW LEVEL SECURITY;

CREATE POLICY watch_tenant_isolation ON watch
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY watch_optout_tenant_isolation ON watch_optout
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY watch_admin_bypass ON watch TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY watch_optout_admin_bypass ON watch_optout TO stator_admin USING (true) WITH CHECK (true);

-- Watching is only ever for oneself, and only of what one may view.
CREATE POLICY watch_own ON watch AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id() AND watch_target_viewable(page_id, space_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND watch_target_viewable(page_id, space_id, current_actor_id()));
CREATE POLICY watch_optout_own ON watch_optout AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON watch, watch_optout TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS page_watchers(uuid);
DROP FUNCTION IF EXISTS page_watch_coverage(uuid, boolean);
DROP FUNCTION IF EXISTS watch_target_viewable(uuid, uuid, uuid);
DROP TABLE IF EXISTS watch_optout;
DROP TABLE IF EXISTS watch;
