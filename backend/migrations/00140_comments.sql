-- +goose Up
-- The discussion on a page. A thread is a first comment and its replies, one
-- level deep; its id is its first comment's id, so a link to either finds it.
-- Below the page (page) now, on a passage of it (inline) with #23, which adds
-- the thread's anchor and whether it is resolved.
CREATE TABLE comment_thread (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    page_id    uuid NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('page', 'inline')),
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    -- The comments name their page too, and must name the thread's.
    UNIQUE (org_id, id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

CREATE INDEX comment_thread_page_idx ON comment_thread (org_id, page_id, id);

-- A deleted comment keeps its row, its author and its place in the thread as
-- a placeholder; its words go in the statement that deletes it, for good.
CREATE TABLE comment (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    thread_id  uuid NOT NULL,
    page_id    uuid NOT NULL,
    author_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    body       jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    edited_at  timestamptz,
    deleted_at timestamptz,
    deleted_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    PRIMARY KEY (org_id, id),
    CONSTRAINT comment_words_until_deleted CHECK ((deleted_at IS NULL) = (body IS NOT NULL)),
    CONSTRAINT comment_body_document CHECK (body IS NULL OR jsonb_typeof(body) = 'object'),
    FOREIGN KEY (org_id, thread_id, page_id) REFERENCES comment_thread (org_id, id, page_id) ON DELETE CASCADE
);

CREATE INDEX comment_thread_idx ON comment (org_id, thread_id, created_at, id);
CREATE INDEX comment_page_idx ON comment (org_id, page_id) WHERE deleted_at IS NULL;

-- Comments are found by their words, read as a page's are, so search reads
-- the same text the service would.
ALTER TABLE comment ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    to_tsvector('stator_search'::regconfig, COALESCE(page_plain_text(body), ''))
) STORED;

CREATE INDEX comment_search_idx ON comment USING gin (search_vector);

-- +goose StatementBegin
-- Commenting on a page: viewing it, the space's addComments, and a page that
-- is published and out of the trash. Edit restrictions do not apply: a
-- comment is not an edit of the page.
CREATE FUNCTION perm_page_commentable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.version > 0 AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addComments'))
$$;

-- Deleting somebody's comment is for whoever holds the space's delete while
-- viewing the page; their own is anybody's who may still view it.
CREATE FUNCTION perm_comment_deletable(target uuid, author uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM page p
            WHERE p.id = target AND p.org_id = current_org_id()
              AND perm_space_holds(actor, p.space_id, 'delete')))
$$;

-- A policy cannot tell an edit from a delete, so this does: the body is the
-- author's alone to change while they may comment, a delete takes the words
-- with it and names who deleted, and nothing else about a comment moves.
CREATE FUNCTION comment_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.thread_id, NEW.page_id, NEW.author_id, NEW.created_at)
       IS DISTINCT FROM (OLD.thread_id, OLD.page_id, OLD.author_id, OLD.created_at) THEN
        RAISE EXCEPTION 'a comment stays where and whose it is' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF OLD.deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'a deleted comment cannot change' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.deleted_at IS NOT NULL THEN
        IF NEW.deleted_by IS DISTINCT FROM actor OR NEW.edited_at IS DISTINCT FROM OLD.edited_at
           OR NOT perm_comment_deletable(OLD.page_id, OLD.author_id, actor) THEN
            RAISE EXCEPTION 'you may not delete that comment' USING ERRCODE = 'insufficient_privilege';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.deleted_by IS NOT NULL OR OLD.author_id IS DISTINCT FROM actor
       OR NOT perm_page_commentable(OLD.page_id, actor) THEN
        RAISE EXCEPTION 'you may not change that comment' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_write_check BEFORE UPDATE ON comment
    FOR EACH ROW EXECUTE FUNCTION comment_write_guard();

ALTER TABLE comment_thread ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment_thread FORCE  ROW LEVEL SECURITY;
ALTER TABLE comment        ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment        FORCE  ROW LEVEL SECURITY;

CREATE POLICY comment_thread_tenant_isolation ON comment_thread
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY comment_tenant_isolation ON comment
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY comment_thread_admin_bypass ON comment_thread TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY comment_admin_bypass ON comment TO stator_admin USING (true) WITH CHECK (true);

-- A comment is read with its page, and written only in one's own name.
CREATE POLICY comment_thread_viewers ON comment_thread AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY comment_thread_starters ON comment_thread AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND perm_page_commentable(page_id, current_actor_id()));
CREATE POLICY comment_viewers ON comment AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY comment_writers ON comment AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (author_id = current_actor_id() AND deleted_at IS NULL AND deleted_by IS NULL AND edited_at IS NULL
                AND perm_page_commentable(page_id, current_actor_id()));
-- What an update may change is comment_write_guard's to say.
CREATE POLICY comment_changers ON comment AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (perm_page_viewable(page_id, current_actor_id()));

-- Nothing is ever deleted outright but with its page, and a thread does not
-- change until #23 resolves it.
GRANT SELECT, INSERT, UPDATE, DELETE ON comment_thread, comment TO stator_admin;
REVOKE ALL ON comment_thread, comment FROM stator_app;
GRANT SELECT, INSERT ON comment_thread, comment TO stator_app;
GRANT UPDATE (body, edited_at, deleted_at, deleted_by) ON comment TO stator_app;

-- +goose Down
DROP TABLE IF EXISTS comment;
DROP TABLE IF EXISTS comment_thread;
DROP FUNCTION IF EXISTS comment_write_guard();
DROP FUNCTION IF EXISTS perm_comment_deletable(uuid, uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_commentable(uuid, uuid);
