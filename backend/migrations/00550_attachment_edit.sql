-- +goose Up
-- An edited picture (#94), cropped or drawn on in the browser, is saved as
-- the name's next version, as a restore is. edited_from names the version it
-- was drawn on, which must be a version of the same name on the same page, a
-- picture of a type the browser redraws, and of the type the edit keeps.
ALTER TABLE attachment ADD COLUMN edited_from integer CHECK (edited_from > 0);
ALTER TABLE attachment ADD CONSTRAINT attachment_restored_or_edited CHECK (restored_from IS NULL OR edited_from IS NULL);

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
    IF NEW.edited_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.edited_from
          AND lower(btrim(split_part(a.content_type, ';', 1))) IN ('image/png', 'image/jpeg', 'image/webp')
          AND lower(btrim(split_part(a.content_type, ';', 1))) = lower(btrim(split_part(NEW.content_type, ';', 1)))) THEN
        RAISE EXCEPTION 'An edited picture is drawn on a PNG, JPEG or WebP version of the same file on the same page, and keeps its type; version % of % is not one.', NEW.edited_from, NEW.file_name
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

ALTER TABLE attachment DROP CONSTRAINT IF EXISTS attachment_restored_or_edited;
ALTER TABLE attachment DROP COLUMN IF EXISTS edited_from;
