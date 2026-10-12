-- +goose Up
-- The outline of a space walked the tree and asked perm_page_viewable of
-- every page, which walks the page's ancestors again, and row level security
-- asked it a second time: a space of 2,000 pages took most of a second (#371).
--
-- A page is viewable when its parent is and nothing of its own forbids it:
-- the ancestors' drafts and view lists are the parent's answer, and what the
-- space grants is the same for every page of it (page_parent_fkey keeps a
-- page's ancestors in its own space). So the walk judges the space once,
-- the root page whole, and each page below only by what it carries itself.
-- It reads as its owner, past row level security, and applies the tenant and
-- viewers policies of page itself: it returns no row the caller could not
-- have read.

-- +goose StatementBegin
-- Root and the pages below it the caller may view, outside the trash and the
-- archive, with their depth and a path that sorts them in reading order.
CREATE FUNCTION page_outline_rows(root uuid)
    RETURNS TABLE (id uuid, parent_id uuid, title text, depth integer, path text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH RECURSIVE judged AS MATERIALIZED (
        SELECT p.id, p.parent_id, p.title, p.rank, a.actor,
               perm_space_holds(a.actor, p.space_id, 'view') AS holds,
               perm_space_holds(a.actor, p.space_id, 'administer') AS administers,
               CASE WHEN a.actor IS NULL THEN perm_link_page() END AS link
        FROM page p CROSS JOIN (SELECT current_actor_id() AS actor) a
        WHERE p.org_id = current_org_id() AND p.id = root AND perm_page_viewable(root, a.actor)
    ), tree (id, parent_id, title, depth, path) AS (
        SELECT j.id, j.parent_id, j.title, 0, j.rank || chr(2) || j.id::text || chr(1)
        FROM judged j
        UNION ALL
        SELECT p.id, p.parent_id, p.title, t.depth + 1, t.path || p.rank || chr(2) || p.id::text || chr(1)
        FROM tree t
        JOIN page p ON p.org_id = current_org_id() AND p.parent_id = t.id
        CROSS JOIN judged j
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL
          AND NOT (p.version = 0 AND p.created_by IS DISTINCT FROM j.actor)
          AND (j.actor IS NOT NULL OR p.version <> 0)
          AND (j.holds OR j.link IS NOT DISTINCT FROM p.id)
          AND (j.administers
               OR NOT EXISTS (SELECT 1 FROM page_restriction r
                              WHERE r.org_id = current_org_id() AND r.page_id = p.id AND r.kind = 'view')
               OR perm_lists_pass(j.actor, ARRAY[p.id], 'view'))
    )
    SELECT id, parent_id, title, depth, path FROM tree
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION page_outline_rows(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_outline_rows(uuid) TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION page_outline_rows(uuid);
