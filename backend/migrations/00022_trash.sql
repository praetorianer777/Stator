-- +goose Up
-- A deleted page goes to its space's trash with every page below it that was
-- still in the tree, as one item named by trash_id, the page deleted. The
-- pages keep their parent and rank, so a restore puts them back where they
-- were; purging the item deletes them for good.
ALTER TABLE page
    ADD COLUMN trashed_at timestamptz,
    ADD COLUMN trashed_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    ADD COLUMN trash_id   uuid,
    ADD CONSTRAINT page_trash_whole CHECK ((trashed_at IS NULL) = (trash_id IS NULL)),
    -- The home page is the root of the tree; a space goes as a whole.
    ADD CONSTRAINT page_home_never_trashed CHECK (parent_id IS NOT NULL OR trashed_at IS NULL),
    ADD CONSTRAINT page_trash_fkey FOREIGN KEY (org_id, trash_id) REFERENCES page (org_id, id) ON DELETE CASCADE;

CREATE INDEX page_trash_roots_idx ON page (org_id, space_id, trashed_at DESC) WHERE trash_id = id;
CREATE INDEX page_trash_items_idx ON page (org_id, trash_id) WHERE trash_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS page_trash_items_idx;
DROP INDEX IF EXISTS page_trash_roots_idx;
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_trash_fkey,
    DROP CONSTRAINT IF EXISTS page_home_never_trashed,
    DROP CONSTRAINT IF EXISTS page_trash_whole,
    DROP COLUMN IF EXISTS trash_id,
    DROP COLUMN IF EXISTS trashed_by,
    DROP COLUMN IF EXISTS trashed_at;
