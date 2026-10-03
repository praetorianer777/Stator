-- +goose Up
-- Versions of a file (#58): a file put on a page under a name the page
-- already has, whatever its case, is that file's next version. Each version
-- keeps its own row and bytes, so a link to one keeps working; the number is
-- the database's to give, so two uploads at once cannot both take it.
ALTER TABLE attachment ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version > 0);

UPDATE attachment a SET version = n.version
FROM (SELECT id, row_number() OVER (PARTITION BY org_id, page_id, lower(file_name) ORDER BY created_at, id) AS version FROM attachment) n
WHERE a.id = n.id;

CREATE UNIQUE INDEX attachment_version_idx ON attachment (org_id, page_id, lower(file_name), version);

-- +goose StatementBegin
-- Definer's rights, so the count sees every version of the name, and the
-- lock serializes uploads of one name to one page.
CREATE FUNCTION attachment_stamp_version() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.page_id::text || '/' || lower(NEW.file_name), 0));
    SELECT COALESCE(max(a.version), 0) + 1 INTO NEW.version
    FROM attachment a
    WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION attachment_stamp_version() FROM PUBLIC;

CREATE TRIGGER attachment_stamp_version BEFORE INSERT ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_stamp_version();

-- +goose Down
DROP TRIGGER IF EXISTS attachment_stamp_version ON attachment;
DROP FUNCTION IF EXISTS attachment_stamp_version();
DROP INDEX IF EXISTS attachment_version_idx;
ALTER TABLE attachment DROP COLUMN IF EXISTS version;
