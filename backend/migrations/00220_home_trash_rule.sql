-- +goose Up
-- Whether a page may go to the trash at all is one rule, which the check on
-- page enforces and the access inspector (#226) asks, so the two cannot drift.

-- +goose StatementBegin
-- A space's home page is the root of its tree and never goes to the trash on
-- its own; the space goes as a whole.
CREATE FUNCTION page_trashable(parent uuid) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT parent IS NOT NULL
$$;
-- +goose StatementEnd

ALTER TABLE page
    DROP CONSTRAINT page_home_never_trashed,
    ADD CONSTRAINT page_home_never_trashed CHECK (page_trashable(parent_id) OR trashed_at IS NULL);

-- +goose Down
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_home_never_trashed,
    ADD CONSTRAINT page_home_never_trashed CHECK (parent_id IS NOT NULL OR trashed_at IS NULL);
DROP FUNCTION IF EXISTS page_trashable(uuid);
