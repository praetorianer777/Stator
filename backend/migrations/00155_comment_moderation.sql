-- +goose Up
-- +goose StatementBegin
-- Deleting somebody else's comment is moderation (#180), so it takes the
-- space's administer; the space's delete is for pages alone. Their own is
-- anybody's who may still view the page. comment_write_guard asks this for
-- every delete, and a blank body without a delete breaks the table's check.
CREATE OR REPLACE FUNCTION perm_comment_deletable(target uuid, author uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM page p
            WHERE p.id = target AND p.org_id = current_org_id()
              AND perm_space_holds(actor, p.space_id, 'administer')))
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_comment_deletable(target uuid, author uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM page p
            WHERE p.id = target AND p.org_id = current_org_id()
              AND perm_space_holds(actor, p.space_id, 'delete')))
$$;
-- +goose StatementEnd
