-- +goose Up
-- Search finds its words through the GIN indexes before it checks who may see
-- the hits, and checks a space at a time where it can (#370). Under row level
-- security the restrictive viewers policy of page, attachment and comment
-- runs perm_page_viewable on every row of the organization first, because @@
-- is not leakproof and so may not run before it: a search cost a permission
-- check per page, however few matched, and again for every join to page.
--
-- These functions read as their owner, which row level security does not
-- hold, so @@ can use the index. Then they apply what the tenant and viewers
-- policies of their table apply, to the matching rows only: they return no
-- row the caller could not have read.

-- +goose StatementBegin
-- Whether no page of the space can answer differently from the space itself
-- for a signed-in actor: none is restricted from viewing, and none is
-- unpublished but by the actor. A page's ancestors are in its own space
-- (page_parent_fkey), so in such a space perm_page_viewable(page, actor) is
-- perm_space_holds(actor, space, 'view'). Readers without an account always
-- take the page by page way, which knows about links.
CREATE FUNCTION perm_space_plain(space uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN page p ON p.org_id = r.org_id AND p.id = r.page_id
                       WHERE r.org_id = current_org_id() AND p.space_id = space AND r.kind = 'view')
       AND NOT EXISTS (SELECT 1 FROM page p
                       WHERE p.org_id = current_org_id() AND p.space_id = space
                         AND p.version = 0 AND p.created_by IS DISTINCT FROM actor)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- The pages matching q that the actor may view, with what a hit shows of them:
-- the search reads no other row of page, so no other check runs per hit.
CREATE FUNCTION page_search_rows(q tsquery)
    RETURNS TABLE (id uuid, org_id uuid, space_id uuid, parent_id uuid, title text, body jsonb, kind text, version integer,
                   archived_at timestamptz, trashed_at timestamptz, changed_at timestamptz, changed_by uuid,
                   rank real)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT p.id, p.space_id, p.parent_id, p.title, p.body, p.kind, p.version, p.archived_at, p.trashed_at,
               ts_rank(p.search_vector, q) AS rank
        FROM page p WHERE p.search_vector @@ q AND p.org_id = current_org_id()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id, current_org_id(), h.space_id, h.parent_id, h.title, h.body, h.kind, h.version, h.archived_at, h.trashed_at,
           v.created_at, v.created_by, h.rank
    FROM hit h
    JOIN judged j ON j.space_id = h.space_id
    JOIN page_version v ON v.org_id = current_org_id() AND v.page_id = h.id AND v.number = h.version
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.id, current_actor_id()) END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION attachment_search_hits(q tsquery) RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT a.id, a.page_id, p.space_id FROM attachment a
        JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
        WHERE a.search_vector @@ q AND a.org_id = current_org_id()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION comment_search_hits(q tsquery) RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT c.id, c.page_id, p.space_id FROM comment c
        JOIN page p ON p.org_id = c.org_id AND p.id = c.page_id
        WHERE c.search_vector @@ q AND c.org_id = current_org_id() AND NOT current_anonymous()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION perm_space_plain(uuid, uuid), page_search_rows(tsquery),
    attachment_search_hits(tsquery), comment_search_hits(tsquery) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION perm_space_plain(uuid, uuid), page_search_rows(tsquery),
    attachment_search_hits(tsquery), comment_search_hits(tsquery) TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION comment_search_hits(tsquery);
DROP FUNCTION attachment_search_hits(tsquery);
DROP FUNCTION page_search_rows(tsquery);
DROP FUNCTION perm_space_plain(uuid, uuid);
