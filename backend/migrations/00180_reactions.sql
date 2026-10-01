-- +goose Up
-- An emoji a member puts on a page or on a comment of it (#66). Each names its
-- page, so the page's view rule reads it, and a comment's reaction names the
-- comment's page, which the foreign key holds it to.
ALTER TABLE comment ADD CONSTRAINT comment_on_page UNIQUE (org_id, id, page_id);

CREATE TABLE reaction (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    page_id    uuid NOT NULL,
    -- Null for a reaction to the page itself.
    comment_id uuid,
    user_id    uuid NOT NULL,
    emoji      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    -- The service holds an emoji to emoji; this keeps words and long strings
    -- out of the table whatever writes it.
    CONSTRAINT reaction_emoji_shape CHECK (
        octet_length(emoji) BETWEEN 1 AND 32 AND emoji !~ '[A-Za-z[:space:][:cntrl:]]'),
    UNIQUE NULLS NOT DISTINCT (org_id, page_id, comment_id, user_id, emoji),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, comment_id, page_id) REFERENCES comment (org_id, id, page_id) ON DELETE CASCADE
);

CREATE INDEX reaction_comment_idx ON reaction (org_id, comment_id) WHERE comment_id IS NOT NULL;

-- +goose StatementBegin
-- A deleted comment is a placeholder, and a placeholder takes no reactions.
-- The share lock makes a reaction and the comment's delete wait for each
-- other, so whichever commits second sees the other.
CREATE FUNCTION reaction_comment_alive() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.org_id = NEW.org_id AND c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'a deleted comment takes no reactions' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

-- The reactions are other people's, which the app role may not delete.
CREATE FUNCTION comment_forget_reactions() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM reaction WHERE org_id = NEW.org_id AND comment_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER reaction_on_live_comment BEFORE INSERT ON reaction
    FOR EACH ROW EXECUTE FUNCTION reaction_comment_alive();

CREATE TRIGGER comment_deleted_forget_reactions AFTER UPDATE OF deleted_at ON comment
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION comment_forget_reactions();

ALTER TABLE reaction ENABLE ROW LEVEL SECURITY;
ALTER TABLE reaction FORCE  ROW LEVEL SECURITY;

CREATE POLICY reaction_tenant_isolation ON reaction
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY reaction_admin_bypass ON reaction TO stator_admin USING (true) WITH CHECK (true);

-- Read with the page; put on only in one's own name, where one may comment;
-- taken off only one's own, while one may still view the page.
CREATE POLICY reaction_viewers ON reaction AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY reaction_reactors ON reaction AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id() AND perm_page_commentable(page_id, current_actor_id()));
CREATE POLICY reaction_removers ON reaction AS RESTRICTIVE FOR DELETE TO stator_app
    USING (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON reaction TO stator_admin;
REVOKE ALL ON reaction FROM stator_app;
GRANT SELECT, INSERT, DELETE ON reaction TO stator_app;

-- +goose Down
DROP TRIGGER IF EXISTS comment_deleted_forget_reactions ON comment;
DROP TABLE IF EXISTS reaction;
DROP FUNCTION IF EXISTS comment_forget_reactions();
DROP FUNCTION IF EXISTS reaction_comment_alive();
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_on_page;
