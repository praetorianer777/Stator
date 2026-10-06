-- +goose Up
-- Blog posts (#72): a post is a page of a third kind that hangs from no page.
-- It has a body, drafts, versions, comments, labels and restrictions as any
-- page does, through the same rows and rules, but no parent: it is outside
-- the tree and belongs to its space's blog, dated by its first publish.
ALTER TABLE page
    DROP CONSTRAINT page_kind_known,
    ADD CONSTRAINT page_kind_known CHECK (kind IN ('page', 'folder', 'post')),
    DROP CONSTRAINT page_home_not_folder,
    ADD CONSTRAINT page_home_not_folder CHECK (kind <> 'folder' OR parent_id IS NOT NULL),
    DROP CONSTRAINT page_folder_empty,
    ADD CONSTRAINT page_folder_empty CHECK (kind <> 'folder' OR body = '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb),
    -- Nothing places a post in the tree, so the tree's moves, copies and
    -- restores can never take one in.
    ADD CONSTRAINT page_post_outside_tree CHECK (kind <> 'post' OR parent_id IS NULL),
    -- When the post was first published, which is its date in the blog.
    ADD COLUMN posted_at timestamptz,
    ADD CONSTRAINT page_posted_only_posts CHECK (kind = 'post' OR posted_at IS NULL);

-- A space still has one root, its home page; its posts have no parent either.
DROP INDEX page_one_root_idx;
CREATE UNIQUE INDEX page_one_root_idx ON page (org_id, space_id) WHERE parent_id IS NULL AND kind <> 'post';

-- A blog is read newest first, in a space or across the organization.
CREATE INDEX page_posts_space_idx ON page (org_id, space_id, posted_at DESC, id DESC) WHERE kind = 'post' AND posted_at IS NOT NULL;
CREATE INDEX page_posts_org_idx ON page (org_id, posted_at DESC, id DESC) WHERE kind = 'post' AND posted_at IS NOT NULL;

-- +goose StatementBegin
-- A post's date is stamped by its first publish and never moves, so the app
-- role cannot backdate a post into a month nobody saw it in. Other roles,
-- the seed and the test fixtures, may set it.
CREATE FUNCTION page_posted_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind <> 'post' THEN
        NEW.posted_at := NULL;
        RETURN NEW;
    END IF;
    IF current_user = 'stator_app' THEN
        NEW.posted_at := CASE WHEN TG_OP = 'UPDATE' THEN OLD.posted_at END;
    END IF;
    IF NEW.posted_at IS NULL AND NEW.version > 0 THEN
        NEW.posted_at := now();
    END IF;
    RETURN NEW;
END;
$$;

-- No page hangs under a post. Read as the table's owner, so a parent the
-- actor cannot see is still found out.
CREATE FUNCTION page_parent_not_post() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL AND EXISTS (SELECT 1 FROM page WHERE id = NEW.parent_id AND kind = 'post') THEN
        RAISE EXCEPTION 'A blog post stays in its blog; no page goes under it.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_under_post';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_posted_stamp BEFORE INSERT OR UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION page_posted_stamp();
CREATE TRIGGER page_parent_not_post BEFORE INSERT OR UPDATE OF parent_id ON page
    FOR EACH ROW EXECUTE FUNCTION page_parent_not_post();

-- +goose StatementBegin
-- As in 00220, and a post goes to the trash like any page below the home
-- page: only the home page stands for its space.
CREATE FUNCTION page_trashable(parent uuid, kind text) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT parent IS NOT NULL OR kind = 'post'
$$;
-- +goose StatementEnd

ALTER TABLE page
    DROP CONSTRAINT page_home_never_trashed,
    ADD CONSTRAINT page_home_never_trashed CHECK (page_trashable(parent_id, kind) OR trashed_at IS NULL),
    DROP CONSTRAINT page_home_never_archived,
    ADD CONSTRAINT page_home_never_archived CHECK (parent_id IS NOT NULL OR kind = 'post' OR archived_at IS NULL);
DROP FUNCTION page_trashable(uuid);

-- +goose StatementBegin
-- As in 00090: the home page takes no view restriction. A post, which has no
-- parent either, takes one like any page.
CREATE OR REPLACE FUNCTION page_restriction_home_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'view' AND EXISTS (SELECT 1 FROM page WHERE id = NEW.page_id AND parent_id IS NULL AND kind <> 'post') THEN
        RAISE EXCEPTION 'the home page takes no view restriction; narrow the space''s permissions instead'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_restriction_home_viewable';
    END IF;
    RETURN NEW;
END;
$$;

-- Writing a post is adding a page to its space: whoever may add pages there
-- may post, while the space is not archived.
CREATE FUNCTION perm_post_insertable(space uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_space_holds(actor, space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.org_id = current_org_id() AND s.archived_at IS NULL)
$$;
-- +goose StatementEnd

-- As in 00090, and a post starts unpublished, so its date is the day its
-- first version went out.
DROP POLICY page_adders ON page;
CREATE POLICY page_adders ON page AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND CASE
        WHEN kind = 'post' THEN version = 0 AND perm_post_insertable(space_id, current_actor_id())
        ELSE perm_page_insertable(space_id, parent_id, current_actor_id())
    END);

-- Watching a space's blog hears of its new posts, apart from watching the
-- whole space.
ALTER TABLE watch
    DROP CONSTRAINT watch_kind_check,
    ADD CONSTRAINT watch_kind_check CHECK (kind IN ('page', 'subtree', 'space', 'blog')),
    DROP CONSTRAINT watch_target,
    ADD CONSTRAINT watch_target CHECK (
        (kind IN ('space', 'blog') AND space_id IS NOT NULL AND page_id IS NULL) OR
        (kind NOT IN ('space', 'blog') AND page_id IS NOT NULL AND space_id IS NULL));
DROP INDEX watch_one_per_space_idx;
CREATE UNIQUE INDEX watch_one_per_space_idx ON watch (org_id, user_id, space_id, kind) WHERE space_id IS NOT NULL;

-- +goose StatementBegin
-- As in 00151, and a post first published also tells whoever watches its
-- space's blog, as a page first published tells whoever watches the page
-- above it. A blog watch is nearer than the space's.
CREATE OR REPLACE FUNCTION page_watch_coverage(target uuid, with_parent boolean)
    RETURNS TABLE (user_id uuid, via text, via_page uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH RECURSIVE up (id, parent_id, space_id, kind, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.kind, 0 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id()
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.kind, up.depth + 1 FROM page p JOIN up ON p.id = up.parent_id
        WHERE p.org_id = current_org_id()
    ), found (user_id, via, via_page, depth) AS (
        SELECT w.user_id, w.kind, CASE WHEN w.kind = 'subtree' OR up.depth > 0 THEN w.page_id END, up.depth
        FROM watch w JOIN up ON w.page_id = up.id
        WHERE w.org_id = current_org_id()
          AND (up.depth = 0 OR w.kind = 'subtree' OR (with_parent AND up.depth = 1))
        UNION ALL
        SELECT w.user_id, 'blog', NULL, 2147483646
        FROM watch w
        WHERE with_parent AND w.org_id = current_org_id() AND w.kind = 'blog'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0 AND kind = 'post')
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
-- +goose StatementEnd

-- A new post in a watched blog or space is a kind of its own, so it can be
-- heard apart from new pages. It follows 00510, which last redefined the
-- kinds, so it lists every one.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'posted', 'expired', 'failed'));

-- +goose Down
DELETE FROM notification WHERE kind = 'posted';
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired', 'failed'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_watch_coverage(target uuid, with_parent boolean)
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
-- +goose StatementEnd

DELETE FROM watch WHERE kind = 'blog';
DROP INDEX IF EXISTS watch_one_per_space_idx;
CREATE UNIQUE INDEX watch_one_per_space_idx ON watch (org_id, user_id, space_id) WHERE space_id IS NOT NULL;
ALTER TABLE watch
    DROP CONSTRAINT watch_target,
    ADD CONSTRAINT watch_target CHECK (
        (kind = 'space' AND space_id IS NOT NULL AND page_id IS NULL) OR
        (kind <> 'space' AND page_id IS NOT NULL AND space_id IS NULL)),
    DROP CONSTRAINT watch_kind_check,
    ADD CONSTRAINT watch_kind_check CHECK (kind IN ('page', 'subtree', 'space'));

DROP POLICY page_adders ON page;
CREATE POLICY page_adders ON page AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND perm_page_insertable(space_id, parent_id, current_actor_id()));
DROP FUNCTION IF EXISTS perm_post_insertable(uuid, uuid);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_restriction_home_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'view' AND EXISTS (SELECT 1 FROM page WHERE id = NEW.page_id AND parent_id IS NULL) THEN
        RAISE EXCEPTION 'the home page takes no view restriction; narrow the space''s permissions instead'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_restriction_home_viewable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION page_trashable(parent uuid) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT parent IS NOT NULL
$$;
-- +goose StatementEnd

DELETE FROM page WHERE kind = 'post';
ALTER TABLE page
    DROP CONSTRAINT page_home_never_archived,
    ADD CONSTRAINT page_home_never_archived CHECK (parent_id IS NOT NULL OR archived_at IS NULL),
    DROP CONSTRAINT page_home_never_trashed,
    ADD CONSTRAINT page_home_never_trashed CHECK (page_trashable(parent_id) OR trashed_at IS NULL);
DROP FUNCTION IF EXISTS page_trashable(uuid, text);

DROP TRIGGER IF EXISTS page_parent_not_post ON page;
DROP TRIGGER IF EXISTS page_posted_stamp ON page;
DROP FUNCTION IF EXISTS page_parent_not_post();
DROP FUNCTION IF EXISTS page_posted_stamp();
DROP INDEX IF EXISTS page_posts_org_idx;
DROP INDEX IF EXISTS page_posts_space_idx;
DROP INDEX IF EXISTS page_one_root_idx;
CREATE UNIQUE INDEX page_one_root_idx ON page (org_id, space_id) WHERE parent_id IS NULL;

ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_posted_only_posts,
    DROP COLUMN IF EXISTS posted_at,
    DROP CONSTRAINT IF EXISTS page_post_outside_tree,
    DROP CONSTRAINT page_folder_empty,
    ADD CONSTRAINT page_folder_empty CHECK (kind = 'page' OR body = '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb),
    DROP CONSTRAINT page_home_not_folder,
    ADD CONSTRAINT page_home_not_folder CHECK (kind = 'page' OR parent_id IS NOT NULL),
    DROP CONSTRAINT page_kind_known,
    ADD CONSTRAINT page_kind_known CHECK (kind IN ('page', 'folder'));
