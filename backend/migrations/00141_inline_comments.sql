-- +goose Up
-- Threads on a passage of a page (#23). The passage is marked in page.body
-- with the mark inlineComment, whose threadId names the thread; the thread
-- keeps the passage's text as its quote, is detached for good once a publish
-- cannot find the passage, and is resolved and reopened by anybody who may
-- comment.
ALTER TABLE comment_thread
    ADD COLUMN quote       text,
    ADD COLUMN detached_at timestamptz,
    ADD COLUMN resolved_at timestamptz,
    ADD COLUMN resolved_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    ADD CONSTRAINT comment_thread_quoted CHECK ((kind = 'inline') = (quote IS NOT NULL)),
    ADD CONSTRAINT comment_thread_quote_length CHECK (char_length(quote) <= 500),
    ADD CONSTRAINT comment_thread_inline_states CHECK (kind = 'inline' OR (detached_at IS NULL AND resolved_at IS NULL)),
    ADD CONSTRAINT comment_thread_resolver CHECK (resolved_at IS NOT NULL OR resolved_by IS NULL);

-- +goose StatementBegin
-- The document with the passage marks of one thread taken out, or of every
-- thread when thread is null, and its text joined again where only a mark
-- set it apart, so that it compares equal to the same words never marked.
-- document.DropMarks in Go normalizes the same way.
CREATE FUNCTION document_without_anchors(node jsonb, thread text, depth integer DEFAULT 0) RETURNS jsonb
    LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
DECLARE
    marks jsonb;
    child jsonb;
    prev  jsonb;
    kept  jsonb := '[]'::jsonb;
BEGIN
    IF jsonb_typeof(node) IS DISTINCT FROM 'object' THEN
        RETURN node;
    END IF;
    IF jsonb_typeof(node -> 'marks') = 'array' THEN
        SELECT COALESCE(jsonb_agg(e.m ORDER BY e.n), '[]'::jsonb) INTO marks
        FROM jsonb_array_elements(node -> 'marks') WITH ORDINALITY AS e (m, n)
        WHERE NOT (e.m ->> 'type' = 'inlineComment' AND (thread IS NULL OR e.m -> 'attrs' ->> 'threadId' = thread));
        node := CASE WHEN marks = '[]'::jsonb THEN node - 'marks' ELSE jsonb_set(node, '{marks}', marks) END;
    END IF;
    IF node -> 'attrs' = '{}'::jsonb THEN
        node := node - 'attrs';
    END IF;
    IF depth > 40 OR jsonb_typeof(node -> 'content') IS DISTINCT FROM 'array' THEN
        RETURN node;
    END IF;
    FOR child IN
        SELECT document_without_anchors(c.value, thread, depth + 1)
        FROM jsonb_array_elements(node -> 'content') WITH ORDINALITY AS c (value, n) ORDER BY c.n
    LOOP
        prev := kept -> -1;
        IF child ->> 'type' = 'text' AND prev ->> 'type' = 'text' AND (child - 'text') = (prev - 'text') THEN
            kept := jsonb_set(kept, '{-1,text}', to_jsonb((prev ->> 'text') || (child ->> 'text')));
        ELSE
            kept := kept || jsonb_build_array(child);
        END IF;
    END LOOP;
    RETURN CASE WHEN kept = '[]'::jsonb THEN node - 'content' ELSE jsonb_set(node, '{content}', kept) END;
END;
$$;
-- +goose StatementEnd

-- A body as a version holds it: without passage marks, and untouched when it
-- has none.
CREATE FUNCTION document_unanchored(body jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT CASE WHEN jsonb_path_exists(body, 'lax $.**.marks[*] ? (@.type == "inlineComment")')
                THEN document_without_anchors(body, NULL) ELSE body END
$$;

-- The thread ids a body's passage marks name.
CREATE FUNCTION document_anchor_ids(body jsonb) RETURNS text[]
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(array_agg(DISTINCT id #>> '{}'), '{}')
    FROM jsonb_path_query(body, 'lax $.**.marks[*] ? (@.type == "inlineComment").attrs.threadId') AS id
$$;

-- +goose StatementBegin
-- Whether a change of a page's body only marks the passage of one new
-- thread, started by the actor on this page: the one change somebody who may
-- comment, and not edit, may make.
CREATE FUNCTION page_anchor_added(target uuid, before jsonb, after jsonb, actor uuid) RETURNS boolean
    LANGUAGE plpgsql STABLE
AS $$
DECLARE
    added text[] := ARRAY(SELECT unnest(document_anchor_ids(after)) EXCEPT SELECT unnest(document_anchor_ids(before)));
BEGIN
    IF cardinality(added) <> 1 OR added[1] !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN false;
    END IF;
    RETURN perm_page_commentable(target, actor)
       AND EXISTS (
           SELECT 1 FROM comment_thread t
           WHERE t.org_id = current_org_id() AND t.id = added[1]::uuid AND t.page_id = target
             AND t.kind = 'inline' AND t.created_by = actor AND t.detached_at IS NULL)
       AND document_without_anchors(after, added[1]) = document_without_anchors(before, added[1]);
END;
$$;

-- As in 00090, but a body may also change by page_anchor_added alone.
CREATE OR REPLACE FUNCTION page_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank)
       IS DISTINCT FROM (OLD.title, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank)
       AND NOT perm_page_editable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.body IS DISTINCT FROM OLD.body AND NOT perm_page_editable(OLD.id, actor)
       AND NOT page_anchor_added(OLD.id, OLD.body, NEW.body, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id AND NOT perm_page_editable(NEW.parent_id, actor) THEN
        RAISE EXCEPTION 'you may not put a page there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.trashed_at, NEW.trash_id, NEW.trashed_by) IS DISTINCT FROM (OLD.trashed_at, OLD.trash_id, OLD.trashed_by)
       AND NOT perm_page_deletable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not delete or restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- Marking a passage is not a change of the page, so it keeps its updated_at.
CREATE FUNCTION page_touch() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.body IS DISTINCT FROM OLD.body
       AND (NEW.title, NEW.version) IS NOT DISTINCT FROM (OLD.title, OLD.version)
       AND document_without_anchors(NEW.body, NULL) = document_without_anchors(OLD.body, NULL) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION strip_version_anchors() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.body := document_unanchored(NEW.body);
    RETURN NEW;
END;
$$;

-- What a thread may change is its resolution, by anybody who may comment,
-- in their own name, and its passage being let go, by whoever publishes the
-- page; a detached thread is never anchored again.
CREATE FUNCTION comment_thread_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.id, NEW.page_id, NEW.kind, NEW.created_by, NEW.created_at, NEW.quote)
       IS DISTINCT FROM (OLD.id, OLD.page_id, OLD.kind, OLD.created_by, OLD.created_at, OLD.quote) THEN
        RAISE EXCEPTION 'a thread stays where and whose it is' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.detached_at IS DISTINCT FROM OLD.detached_at
       AND (OLD.detached_at IS NOT NULL OR NOT perm_page_editable(OLD.page_id, actor)) THEN
        RAISE EXCEPTION 'a passage is let go only by publishing the page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.resolved_at, NEW.resolved_by) IS DISTINCT FROM (OLD.resolved_at, OLD.resolved_by)
       AND (NOT perm_page_commentable(OLD.page_id, actor)
            OR (NEW.resolved_at IS NOT NULL AND NEW.resolved_by IS DISTINCT FROM actor)) THEN
        RAISE EXCEPTION 'you may not resolve or reopen that thread' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER page_set_updated_at ON page;
CREATE TRIGGER page_set_updated_at BEFORE UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION page_touch();

-- Versions never hold passage marks, whoever writes them: the live anchors
-- are in page.body alone.
CREATE TRIGGER page_version_strip_anchors BEFORE INSERT OR UPDATE OF body ON page_version
    FOR EACH ROW EXECUTE FUNCTION strip_version_anchors();

CREATE TRIGGER comment_thread_write_check BEFORE UPDATE ON comment_thread
    FOR EACH ROW EXECUTE FUNCTION comment_thread_write_guard();

CREATE POLICY comment_thread_fresh ON comment_thread AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (detached_at IS NULL AND resolved_at IS NULL AND resolved_by IS NULL);
-- What an update may change is comment_thread_write_guard's to say.
CREATE POLICY comment_thread_changers ON comment_thread AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (perm_page_viewable(page_id, current_actor_id()));

GRANT UPDATE (detached_at, resolved_at, resolved_by) ON comment_thread TO stator_app;

-- +goose Down
REVOKE UPDATE (detached_at, resolved_at, resolved_by) ON comment_thread FROM stator_app;
DROP POLICY IF EXISTS comment_thread_changers ON comment_thread;
DROP POLICY IF EXISTS comment_thread_fresh ON comment_thread;
DROP TRIGGER IF EXISTS comment_thread_write_check ON comment_thread;
DROP TRIGGER IF EXISTS page_version_strip_anchors ON page_version;
DROP TRIGGER IF EXISTS page_set_updated_at ON page;
CREATE TRIGGER page_set_updated_at BEFORE UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.body, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank)
       IS DISTINCT FROM (OLD.title, OLD.body, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank)
       AND NOT perm_page_editable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not change that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id AND NOT perm_page_editable(NEW.parent_id, actor) THEN
        RAISE EXCEPTION 'you may not put a page there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF (NEW.trashed_at, NEW.trash_id, NEW.trashed_by) IS DISTINCT FROM (OLD.trashed_at, OLD.trash_id, OLD.trashed_by)
       AND NOT perm_page_deletable(OLD.id, actor) THEN
        RAISE EXCEPTION 'you may not delete or restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS comment_thread_write_guard();
DROP FUNCTION IF EXISTS strip_version_anchors();
DROP FUNCTION IF EXISTS page_touch();
DROP FUNCTION IF EXISTS page_anchor_added(uuid, jsonb, jsonb, uuid);
DROP FUNCTION IF EXISTS document_anchor_ids(jsonb);
DROP FUNCTION IF EXISTS document_unanchored(jsonb);
DROP FUNCTION IF EXISTS document_without_anchors(jsonb, text, integer);
ALTER TABLE comment_thread
    DROP CONSTRAINT IF EXISTS comment_thread_resolver,
    DROP CONSTRAINT IF EXISTS comment_thread_inline_states,
    DROP CONSTRAINT IF EXISTS comment_thread_quote_length,
    DROP CONSTRAINT IF EXISTS comment_thread_quoted,
    DROP COLUMN IF EXISTS resolved_by,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS detached_at,
    DROP COLUMN IF EXISTS quote;
