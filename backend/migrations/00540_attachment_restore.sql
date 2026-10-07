-- +goose Up
-- Restoring a file's version (#95) uploads that version's bytes again as the
-- name's next version, so history stays append only and a restore is one more
-- row. restored_from names the version it brought back, which must be an
-- earlier version of the same name on the same page when the row is written.
ALTER TABLE attachment ADD COLUMN restored_from integer CHECK (restored_from > 0);

-- +goose StatementBegin
-- Definer's rights, so the count sees every version of the name, and the
-- lock serializes uploads of one name to one page.
CREATE OR REPLACE FUNCTION attachment_stamp_version() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.page_id::text || '/' || lower(NEW.file_name), 0));
    SELECT COALESCE(max(a.version), 0) + 1 INTO NEW.version
    FROM attachment a
    WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name);
    IF NEW.restored_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.restored_from AND a.version < NEW.version - 1) THEN
        RAISE EXCEPTION 'A restore brings back an earlier version of the same file on the same page, and version % of % is not one.', NEW.restored_from, NEW.file_name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_stamp_version() RETURNS trigger
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

ALTER TABLE attachment DROP COLUMN IF EXISTS restored_from;
