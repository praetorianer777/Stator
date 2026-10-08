-- +goose Up
-- An import makes a new space of a space export another wiki wrote, as well
-- as of a Stator archive (#91): the same job, read by another reader. Such an
-- export names no key Stator would take, so its importer always chooses one.
ALTER TABLE space_import ADD COLUMN source text NOT NULL DEFAULT 'archive';
ALTER TABLE space_import ADD CONSTRAINT space_import_source_known CHECK (source IN ('archive', 'html', 'xml'));
ALTER TABLE space_import ADD CONSTRAINT space_import_export_keyed CHECK (source = 'archive' OR key IS NOT NULL);
GRANT INSERT (source) ON space_import TO stator_app;

-- +goose Down
REVOKE INSERT (source) ON space_import FROM stator_app;
ALTER TABLE space_import DROP COLUMN IF EXISTS source;
