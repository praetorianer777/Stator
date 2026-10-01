-- +goose Up
-- The stale content report (#99): published pages nobody opened or published
-- for a while, for the administrators of their spaces.

-- The last view of a page by anybody is one probe of this index rather than a
-- read of every visitor's row. It also serves what page_visit_page_idx did.
CREATE INDEX page_visit_latest_idx ON page_visit (org_id, page_id, visited_at DESC);
DROP INDEX page_visit_page_idx;

-- +goose StatementBegin
-- Published pages out of the trash, in the spaces the actor administers, that
-- nobody published or opened for in_days days, the stalest first after the
-- keyset (after_at, after_id). Visits are everybody's, which only this
-- function reads; it says when, never who. The cheap filters and the sort run
-- before perm_page_viewable, which then runs on the sorted rows until the
-- window is full: row level security would have run its policy on every page.
CREATE FUNCTION stale_pages(in_space uuid, in_owner uuid, in_unowned boolean, in_verification text,
                            in_days integer, after_at timestamptz, after_id uuid, max_rows integer)
    RETURNS TABLE (page_id uuid, title text, space_key text, space_name text, version integer,
                   published_at timestamptz, viewed_at timestamptz, active_at timestamptz,
                   owner_id uuid, owner_name text, owner_can_view boolean,
                   verification_state text, verification_expires_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH reviewed AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.org_id = current_org_id() AND (in_space IS NULL OR s.id = in_space)
          AND perm_space_holds(current_actor_id(), s.id, 'administer')
    ), cut AS MATERIALIZED (
        SELECT now() - make_interval(days => in_days) AS at
    ), candidates AS (
        SELECT p.id, p.title, p.space_id, p.version, p.published_at, lv.at AS viewed_at,
               greatest(p.published_at, lv.at) AS active_at
        FROM page p
        LEFT JOIN LATERAL (
            SELECT max(v.visited_at) AS at FROM page_visit v
            WHERE v.org_id = current_org_id() AND v.page_id = p.id
        ) lv ON true
        WHERE p.org_id = current_org_id() AND p.published_at IS NOT NULL AND p.trashed_at IS NULL
          AND p.published_at < (SELECT at FROM cut)
          AND p.space_id IN (SELECT id FROM reviewed)
          AND (lv.at IS NULL OR lv.at < (SELECT at FROM cut))
          AND (in_owner IS NULL OR EXISTS (
                SELECT 1 FROM page_owner o
                WHERE o.org_id = current_org_id() AND o.page_id = p.id AND o.user_id = in_owner))
          AND (NOT in_unowned OR NOT EXISTS (
                SELECT 1 FROM page_owner o WHERE o.org_id = current_org_id() AND o.page_id = p.id))
          AND CASE in_verification
                WHEN 'verified' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.org_id = current_org_id() AND pv.page_id = p.id AND pv.expires_at > now())
                WHEN 'expired' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.org_id = current_org_id() AND pv.page_id = p.id AND pv.expires_at <= now())
                WHEN 'none' THEN NOT EXISTS (
                    SELECT 1 FROM page_verification pv WHERE pv.org_id = current_org_id() AND pv.page_id = p.id)
                ELSE true
              END
    ), picked AS (
        SELECT o.* FROM (
            SELECT c.* FROM candidates c
            WHERE after_at IS NULL OR (c.active_at, c.id) > (after_at, after_id)
            ORDER BY c.active_at, c.id
            -- OFFSET 0 keeps the view rule out of this subquery, so it runs
            -- above the sort, on as many rows as the window needs.
            OFFSET 0
        ) o
        WHERE perm_page_viewable(o.id, current_actor_id())
        LIMIT max_rows
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.viewed_at, k.active_at,
           ow.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''),
           CASE WHEN ow.user_id IS NULL THEN false ELSE perm_page_viewable(k.id, ow.user_id) END,
           CASE WHEN pv.page_id IS NULL THEN 'none' WHEN pv.expires_at > now() THEN 'verified' ELSE 'expired' END,
           pv.expires_at
    FROM picked k
    JOIN space s ON s.org_id = current_org_id() AND s.id = k.space_id
    LEFT JOIN page_owner ow ON ow.org_id = current_org_id() AND ow.page_id = k.id
    LEFT JOIN app_user u ON u.id = ow.user_id
    LEFT JOIN page_verification pv ON pv.org_id = current_org_id() AND pv.page_id = k.id
    ORDER BY k.active_at, k.id
$$;

-- Whether the actor administers any space, which is who may read the report
-- at all; the organization's administrators administer every one.
CREATE FUNCTION stale_reviewer() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM space s
                   WHERE s.org_id = current_org_id() AND perm_space_holds(current_actor_id(), s.id, 'administer'))
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION stale_pages(uuid, uuid, boolean, text, integer, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION stale_reviewer() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION stale_pages(uuid, uuid, boolean, text, integer, timestamptz, uuid, integer) TO stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION stale_reviewer() TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS stale_reviewer();
DROP FUNCTION IF EXISTS stale_pages(uuid, uuid, boolean, text, integer, timestamptz, uuid, integer);
CREATE INDEX IF NOT EXISTS page_visit_page_idx ON page_visit (org_id, page_id);
DROP INDEX IF EXISTS page_visit_latest_idx;
