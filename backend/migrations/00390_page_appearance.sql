-- +goose Up
-- A page's appearance (#50): an emoji before its title and in the tree, a
-- width, and a cover picture with the point it keeps in view. They are the
-- page's own properties, not versions of its words, so they change at once.
ALTER TABLE attachment
    -- What a cover names: a file of the page itself.
    ADD CONSTRAINT attachment_page_id_key UNIQUE (page_id, id);

ALTER TABLE page
    ADD COLUMN icon text,
    ADD COLUMN width text NOT NULL DEFAULT 'fixed',
    ADD COLUMN cover_attachment_id uuid,
    ADD COLUMN cover_focus_x smallint NOT NULL DEFAULT 50,
    ADD COLUMN cover_focus_y smallint NOT NULL DEFAULT 50,
    -- One emoji, perhaps of several code points, never words: the service
    -- holds it to emoji, and the database to a short run without spaces.
    ADD CONSTRAINT page_icon_short CHECK (icon IS NULL OR icon ~ '^[^[:space:][:cntrl:]]{1,16}$'),
    ADD CONSTRAINT page_width CHECK (width IN ('fixed', 'full')),
    ADD CONSTRAINT page_cover_focus CHECK (cover_focus_x BETWEEN 0 AND 100 AND cover_focus_y BETWEEN 0 AND 100),
    -- The cover is a file of this page; deleting the file takes the cover away.
    ADD CONSTRAINT page_cover_fkey FOREIGN KEY (id, cover_attachment_id) REFERENCES attachment (page_id, id) ON DELETE SET NULL (cover_attachment_id);

CREATE INDEX page_cover_idx ON page (cover_attachment_id) WHERE cover_attachment_id IS NOT NULL;

-- +goose StatementBegin
-- The appearance is editing the page, so it needs edit as its words do.
-- Only the app role is held to it; the foreign key's cascade runs as another.
CREATE FUNCTION page_appearance_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_page_editable(OLD.id, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not change how that page looks' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_appearance_check
    BEFORE UPDATE OF icon, width, cover_attachment_id, cover_focus_x, cover_focus_y ON page
    FOR EACH ROW
    WHEN ((NEW.icon, NEW.width, NEW.cover_attachment_id, NEW.cover_focus_x, NEW.cover_focus_y)
          IS DISTINCT FROM (OLD.icon, OLD.width, OLD.cover_attachment_id, OLD.cover_focus_x, OLD.cover_focus_y))
    EXECUTE FUNCTION page_appearance_guard();

-- +goose Down
DROP TRIGGER page_appearance_check ON page;
DROP FUNCTION page_appearance_guard();
DROP INDEX page_cover_idx;
ALTER TABLE page
    DROP CONSTRAINT page_cover_fkey,
    DROP COLUMN cover_focus_y,
    DROP COLUMN cover_focus_x,
    DROP COLUMN cover_attachment_id,
    DROP COLUMN width,
    DROP COLUMN icon;
ALTER TABLE attachment DROP CONSTRAINT attachment_page_id_key;
