-- +goose Up
-- A person's own rows that name a page, read without asking whether they may
-- view it: their page views (#97) and the shares they sent. A token limited
-- to spaces reads only those about pages in its spaces; a session and any
-- other token read them as before, pages they may no longer view included.

-- +goose StatementBegin
CREATE FUNCTION perm_token_reaches_page(target uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_whole(current_actor_id()) OR EXISTS (
        SELECT 1 FROM page p
        WHERE p.org_id = current_org_id() AND p.id = target
          AND perm_token_reaches(current_actor_id(), p.space_id))
$$;
-- +goose StatementEnd

ALTER POLICY page_view_own ON page_view
    USING (user_id = current_actor_id() AND perm_token_reaches_page(page_id));

ALTER POLICY page_share_own ON page_share
    USING (sharer_id = current_actor_id() AND perm_token_reaches_page(page_id));

-- +goose Down
ALTER POLICY page_share_own ON page_share USING (sharer_id = current_actor_id());
ALTER POLICY page_view_own ON page_view USING (user_id = current_actor_id());
DROP FUNCTION IF EXISTS perm_token_reaches_page(uuid);
