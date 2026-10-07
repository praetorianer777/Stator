-- +goose Up
-- Folders (#36): a row of the page tree that holds pages and folders and
-- nothing of its own, so a group needs no empty page above it. A folder is a
-- page of another kind, so moving, ranking, the trash, archive and
-- restrictions treat it as they treat a page.
ALTER TABLE page
    ADD COLUMN kind text NOT NULL DEFAULT 'page',
    ADD CONSTRAINT page_kind_known CHECK (kind IN ('page', 'folder')),
    -- A space opens on its home page, which has something to say.
    ADD CONSTRAINT page_home_not_folder CHECK (kind = 'page' OR parent_id IS NOT NULL),
    ADD CONSTRAINT page_folder_empty CHECK (kind = 'page' OR body = '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb);

-- +goose StatementBegin
-- A page that became a folder would lose its text, and a folder that became a
-- page would have none: what a row is stays what it was made as.
CREATE FUNCTION page_kind_fixed() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.kind IS DISTINCT FROM OLD.kind THEN
        RAISE EXCEPTION 'A page stays a page and a folder a folder.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_kind_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_kind_fixed BEFORE UPDATE OF kind ON page
    FOR EACH ROW EXECUTE FUNCTION page_kind_fixed();

-- +goose StatementBegin
-- Nothing that belongs to a page's content belongs to a folder: no versions,
-- drafts, files, labels, comments, reactions, shares or stewardship.
CREATE FUNCTION page_not_folder() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM page WHERE id = NEW.page_id AND kind = 'folder') THEN
        RAISE EXCEPTION 'A folder holds pages, not content of its own.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_is_folder';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_version_not_folder BEFORE INSERT ON page_version FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER page_draft_not_folder BEFORE INSERT ON page_draft FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER attachment_not_folder BEFORE INSERT ON attachment FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER page_label_not_folder BEFORE INSERT ON page_label FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER comment_thread_not_folder BEFORE INSERT ON comment_thread FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER comment_not_folder BEFORE INSERT ON comment FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER reaction_not_folder BEFORE INSERT ON reaction FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER page_share_not_folder BEFORE INSERT ON page_share FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER page_owner_not_folder BEFORE INSERT ON page_owner FOR EACH ROW EXECUTE FUNCTION page_not_folder();
CREATE TRIGGER page_verification_not_folder BEFORE INSERT ON page_verification FOR EACH ROW EXECUTE FUNCTION page_not_folder();

-- +goose StatementBegin
-- As in 00210, but a folder is never published: it has nothing to publish, so
-- it stays out of the home page's feeds and the stale report, which go by
-- published_at. Its version is 1 only so that everybody who may see its
-- place in the tree sees it.
CREATE OR REPLACE FUNCTION page_published_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'folder' THEN
        NEW.published_at := NULL;
    ELSIF TG_OP = 'INSERT' THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSE
        NEW.published_at := OLD.published_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_published_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSE
        NEW.published_at := OLD.published_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS page_version_not_folder ON page_version;
DROP TRIGGER IF EXISTS page_draft_not_folder ON page_draft;
DROP TRIGGER IF EXISTS attachment_not_folder ON attachment;
DROP TRIGGER IF EXISTS page_label_not_folder ON page_label;
DROP TRIGGER IF EXISTS comment_thread_not_folder ON comment_thread;
DROP TRIGGER IF EXISTS comment_not_folder ON comment;
DROP TRIGGER IF EXISTS reaction_not_folder ON reaction;
DROP TRIGGER IF EXISTS page_share_not_folder ON page_share;
DROP TRIGGER IF EXISTS page_owner_not_folder ON page_owner;
DROP TRIGGER IF EXISTS page_verification_not_folder ON page_verification;
DROP FUNCTION IF EXISTS page_not_folder();
DROP TRIGGER IF EXISTS page_kind_fixed ON page;
DROP FUNCTION IF EXISTS page_kind_fixed();
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_folder_empty,
    DROP CONSTRAINT IF EXISTS page_home_not_folder,
    DROP CONSTRAINT IF EXISTS page_kind_known,
    DROP COLUMN IF EXISTS kind;
