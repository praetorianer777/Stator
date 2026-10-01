-- +goose Up
-- The language a person reads the interface in (#113). The column came with
-- the first migration, defaulted to English and was never read or written, so
-- every stored value is that default and none is a choice: they become null,
-- which means following the browser. A choice is one of the languages the
-- interface speaks, whatever writes it.
ALTER TABLE app_user ALTER COLUMN locale DROP NOT NULL, ALTER COLUMN locale DROP DEFAULT;
UPDATE app_user SET locale = NULL;
ALTER TABLE app_user ADD CONSTRAINT app_user_locale_offered CHECK (locale IN ('en', 'de'));

-- +goose Down
ALTER TABLE app_user DROP CONSTRAINT IF EXISTS app_user_locale_offered;
UPDATE app_user SET locale = 'en' WHERE locale IS NULL;
ALTER TABLE app_user ALTER COLUMN locale SET DEFAULT 'en', ALTER COLUMN locale SET NOT NULL;
